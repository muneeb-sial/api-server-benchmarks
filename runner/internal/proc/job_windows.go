//go:build windows

package proc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Job object notification message ids (winnt.h).
const (
	msgActiveProcessZero  = 4
	msgAbnormalExit       = 8
	msgProcessMemoryLimit = 9
	msgJobMemoryLimit     = 10

	processSuspendResume = 0x0800
	maxPIDs              = 4096
)

var (
	ntdll               = windows.NewLazySystemDLL("ntdll.dll")
	procNtResumeProcess = ntdll.NewProc("NtResumeProcess")
	kernel32            = windows.NewLazySystemDLL("kernel32.dll")
	procGetProcMemInfo  = kernel32.NewProc("K32GetProcessMemoryInfo")
)

type basicAccounting struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

type pidList struct {
	NumberOfAssignedProcesses uint32
	NumberOfProcessIdsInList  uint32
	ProcessIdList             [maxPIDs]uintptr
}

type associatePort struct {
	CompletionKey  uintptr
	CompletionPort windows.Handle
}

type memCountersEx struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
	PrivateUsage               uintptr
}

// Job is a running process tree confined to a Job Object.
type Job struct {
	job  windows.Handle
	port windows.Handle
	cmd  *exec.Cmd

	memLimitHit  atomic.Bool
	abnormalExit atomic.Bool

	exited  chan struct{}
	waitErr error

	closeOnce sync.Once
}

// Start creates the job, starts the process suspended, assigns it to the job
// and only then resumes it — so no child can be spawned outside the job.
func Start(o Options) (*Job, error) {
	if len(o.Argv) == 0 {
		return nil, errors.New("empty argv")
	}
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("CreateJobObject: %w", err)
	}
	j := &Job{job: h, exited: make(chan struct{})}
	ok := false
	defer func() {
		if !ok {
			j.Close()
		}
	}()

	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if o.MemoryBytes > 0 {
		info.BasicLimitInformation.LimitFlags |= windows.JOB_OBJECT_LIMIT_JOB_MEMORY
		info.JobMemoryLimit = uintptr(o.MemoryBytes)
	}
	if o.Affinity != 0 {
		info.BasicLimitInformation.LimitFlags |= windows.JOB_OBJECT_LIMIT_AFFINITY
		info.BasicLimitInformation.Affinity = uintptr(o.Affinity)
	}
	if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return nil, fmt.Errorf("set job limits: %w", err)
	}

	port, err := windows.CreateIoCompletionPort(windows.InvalidHandle, 0, 0, 1)
	if err != nil {
		return nil, fmt.Errorf("CreateIoCompletionPort: %w", err)
	}
	j.port = port
	assoc := associatePort{CompletionKey: uintptr(h), CompletionPort: port}
	if _, err := windows.SetInformationJobObject(h, windows.JobObjectAssociateCompletionPortInformation,
		uintptr(unsafe.Pointer(&assoc)), uint32(unsafe.Sizeof(assoc))); err != nil {
		return nil, fmt.Errorf("associate completion port: %w", err)
	}
	go j.watchPort()

	cmd := exec.Command(resolveExe(o.Argv[0], o.Dir), o.Argv[1:]...)
	cmd.Dir = o.Dir
	cmd.Env = append(os.Environ(), o.Env...)
	cmd.Stdout = o.Stdout
	cmd.Stderr = o.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_SUSPENDED | windows.CREATE_NEW_PROCESS_GROUP,
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	j.cmd = cmd

	ph, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|
		processSuspendResume|windows.PROCESS_QUERY_INFORMATION, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("OpenProcess: %w", err)
	}
	defer windows.CloseHandle(ph)
	if err := windows.AssignProcessToJobObject(h, ph); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("AssignProcessToJobObject: %w", err)
	}
	if r, _, _ := procNtResumeProcess.Call(uintptr(ph)); r != 0 {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("NtResumeProcess: status 0x%x", r)
	}

	go func() {
		j.waitErr = cmd.Wait()
		close(j.exited)
	}()
	ok = true
	return j, nil
}

func (j *Job) watchPort() {
	for {
		var msg uint32
		var key uintptr
		var ov *windows.Overlapped
		if err := windows.GetQueuedCompletionStatus(j.port, &msg, &key, &ov, windows.INFINITE); err != nil {
			return // port closed
		}
		switch msg {
		case msgJobMemoryLimit, msgProcessMemoryLimit:
			j.memLimitHit.Store(true)
		case msgAbnormalExit:
			j.abnormalExit.Store(true)
		}
	}
}

// Pid of the root process.
func (j *Job) Pid() int { return j.cmd.Process.Pid }

// Exited is closed when the root process exits.
func (j *Job) Exited() <-chan struct{} { return j.exited }

// Wait blocks until the root process exits.
func (j *Job) Wait() error { <-j.exited; return j.waitErr }

// MemoryLimitHit reports whether any allocation in the tree was refused by the cap.
func (j *Job) MemoryLimitHit() bool { return j.memLimitHit.Load() }

// AbnormalExit reports whether any process in the tree crashed.
func (j *Job) AbnormalExit() bool { return j.abnormalExit.Load() }

// Stats samples CPU time and memory of the whole tree.
func (j *Job) Stats() (Stats, error) {
	var s Stats
	var acct basicAccounting
	if err := windows.QueryInformationJobObject(j.job, windows.JobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&acct)), uint32(unsafe.Sizeof(acct)), nil); err != nil {
		return s, fmt.Errorf("query accounting: %w", err)
	}
	s.CPUTime = time.Duration(acct.TotalUserTime+acct.TotalKernelTime) * 100 // 100ns ticks
	s.Processes = int(acct.ActiveProcesses)

	var ext windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	if err := windows.QueryInformationJobObject(j.job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&ext)), uint32(unsafe.Sizeof(ext)), nil); err == nil {
		s.PeakCommit = uint64(ext.PeakJobMemoryUsed)
	}

	var pids pidList
	if err := windows.QueryInformationJobObject(j.job, windows.JobObjectBasicProcessIdList,
		uintptr(unsafe.Pointer(&pids)), uint32(unsafe.Sizeof(pids)), nil); err != nil {
		return s, fmt.Errorf("query pids: %w", err)
	}
	for i := uint32(0); i < pids.NumberOfProcessIdsInList; i++ {
		ws, priv, err := processMemory(uint32(pids.ProcessIdList[i]))
		if err != nil {
			continue // process exited between listing and opening
		}
		s.WorkingSet += ws
		s.Private += priv
	}
	return s, nil
}

func processMemory(pid uint32) (workingSet, private uint64, err error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return 0, 0, err
	}
	defer windows.CloseHandle(h)
	var mc memCountersEx
	mc.CB = uint32(unsafe.Sizeof(mc))
	if r, _, e := procGetProcMemInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&mc)), uintptr(mc.CB)); r == 0 {
		return 0, 0, e
	}
	return uint64(mc.WorkingSetSize), uint64(mc.PrivateUsage), nil
}

// Close kills every process in the tree and releases the job.
func (j *Job) Close() {
	j.closeOnce.Do(func() {
		if j.job != 0 {
			_ = windows.TerminateJobObject(j.job, 1)
		}
		if j.cmd != nil && j.cmd.Process != nil {
			select {
			case <-j.exited:
			case <-time.After(5 * time.Second):
			}
		}
		if j.port != 0 {
			windows.CloseHandle(j.port)
		}
		if j.job != 0 {
			windows.CloseHandle(j.job)
		}
	})
}
