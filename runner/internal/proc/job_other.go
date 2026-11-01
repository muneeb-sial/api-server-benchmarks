//go:build !windows

package proc

import "errors"

// Job is not implemented outside Windows yet (a cgroup v2 backend would go here).
type Job struct{}

func Start(Options) (*Job, error) {
	return nil, errors.New("proc: only Windows Job Objects are implemented")
}

func (j *Job) Pid() int                { return 0 }
func (j *Job) Exited() <-chan struct{} { return nil }
func (j *Job) Wait() error             { return nil }
func (j *Job) MemoryLimitHit() bool    { return false }
func (j *Job) AbnormalExit() bool      { return false }
func (j *Job) Stats() (Stats, error)   { return Stats{}, errors.New("unsupported") }
func (j *Job) Close()                  {}
