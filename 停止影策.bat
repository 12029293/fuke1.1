@echo off
setlocal EnableExtensions
title Yingce Stopper

echo ================================================
echo   Yingce Stopper
echo ================================================

rem ---- ports must match 启动影策.bat ----
set "WEB_PORT=3000"

rem ---- stop backend by image name ----
taskkill /F /IM yingce-server.exe >nul 2>&1
if not errorlevel 1 (echo [ok] backend stopped) else (echo [info] backend not running)

rem ---- stop frontend by listening port (only that port) ----
set "KILLED="
for /f "tokens=5" %%p in ('netstat -ano ^| findstr /C:":%WEB_PORT% " ^| findstr LISTENING') do (
    taskkill /F /T /PID %%p >nul 2>&1
    set "KILLED=1"
)
if defined KILLED (echo [ok] frontend stopped) else (echo [info] frontend not running)

echo.
echo Done.
timeout /t 3 >NUL
endlocal
