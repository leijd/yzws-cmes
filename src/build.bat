@echo off
setlocal EnableExtensions

cd /d "%~dp0"

where go >nul 2>nul
if errorlevel 1 (
    echo [ERROR] Go is not installed or is not available in PATH.
    exit /b 1
)

if not exist ".build" mkdir ".build"
set "CGO_ENABLED=0"

call :build linux amd64 clothp-linux-amd64
if errorlevel 1 goto :failed
call :build linux arm64 clothp-linux-arm64
if errorlevel 1 goto :failed
call :build windows amd64 clothp-windows-amd64.exe
if errorlevel 1 goto :failed
call :build windows arm64 clothp-windows-arm64.exe
if errorlevel 1 goto :failed

echo.
echo [SUCCESS] All backend binaries were built in %CD%\.build
exit /b 0

:build
set "GOOS=%~1"
set "GOARCH=%~2"
echo [BUILD] %GOOS%/%GOARCH%
go build -trimpath -o ".build\%~3" ./cmd/server
if errorlevel 1 exit /b 1
exit /b 0

:failed
echo.
echo [ERROR] Backend build failed for %GOOS%/%GOARCH%.
exit /b 1
