@echo off
REM Portable Media Streamer Launcher for Windows
REM Auto-detects drive location and starts PMS

setlocal enabledelayedexpansion

set "SCRIPT_DIR=%~dp0"
set "BINARY=%SCRIPT_DIR%bin\pms.exe"
if not defined PMS_PORT set "PMS_PORT=8080"
if not defined PMS_LOG_LEVEL set "PMS_LOG_LEVEL=info"

if not exist "%BINARY%" (
    echo Error: PMS binary not found at %BINARY%
    echo.
    echo Please build the Windows binary:
    echo   GOOS=windows GOARCH=amd64 go build -o bin/pms.exe src/cmd/pms/main.go
    pause
    exit /b 1
)

echo ================================================
echo   Portable Media Streamer
echo ================================================
echo   Drive Location: %SCRIPT_DIR%
echo   Port: %PMS_PORT%
echo   Log Level: %PMS_LOG_LEVEL%
echo ================================================
echo.
echo Starting server...
echo Access at: http://localhost:%PMS_PORT%
echo Press Ctrl+C to stop
echo.

"%BINARY%" --path "%SCRIPT_DIR%" --port %PMS_PORT% --log-level %PMS_LOG_LEVEL%

pause
