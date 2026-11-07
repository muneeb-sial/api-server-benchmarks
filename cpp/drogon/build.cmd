@echo off
rem Release build with the MSVC toolchain, CMake and Ninja that ship with Visual Studio.
rem Dependencies come from a vcpkg checkout pinned below, cloned into <repo>\.vcpkg on first use
rem (Visual Studio's bundled vcpkg pairs old helper scripts with ports that no longer match).
setlocal
set "VCPKG_COMMIT=9a5dfef5d1a0438a7d24856a85bd60f309d355e9"
for %%i in ("%~dp0..\..\.vcpkg") do set "VCPKG=%%~fi"

set "PATH=%ProgramFiles(x86)%\Microsoft Visual Studio\Installer;%PATH%"
for /f "usebackq delims=" %%i in (`vswhere -latest -products * -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath`) do set "VS=%%i"
if not defined VS (
  echo Visual Studio C++ build tools not found
  exit /b 1
)
call "%VS%\VC\Auxiliary\Build\vcvars64.bat" >nul || exit /b 1

if not exist "%VCPKG%\vcpkg.exe" (
  git init -q "%VCPKG%" || exit /b 1
  git -C "%VCPKG%" fetch -q --depth 1 https://github.com/microsoft/vcpkg %VCPKG_COMMIT% || exit /b 1
  git -C "%VCPKG%" checkout -q FETCH_HEAD || exit /b 1
  call "%VCPKG%\bootstrap-vcpkg.bat" -disableMetrics || exit /b 1
)
set "VCPKG_ROOT=%VCPKG%"

cmake -S . -B build -G Ninja -DCMAKE_BUILD_TYPE=Release ^
  -DCMAKE_TOOLCHAIN_FILE="%VCPKG%\scripts\buildsystems\vcpkg.cmake" ^
  -DVCPKG_TARGET_TRIPLET=x64-windows-static-release || exit /b 1
cmake --build build || exit /b 1
