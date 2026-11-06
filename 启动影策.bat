@echo off
rem Delayed expansion is REQUIRED for the DATABASE_URL build below.
rem It lets us expand a bare & into the value AFTER cmd parses the line, so the
rem ampersand survives literally while no ^ escape character leaks into the env.
setlocal EnableExtensions EnableDelayedExpansion
title Yingce Launcher
cd /d "%~dp0"

echo ================================================
echo   Yingce (open-ai-canvas) Launcher
echo ================================================

rem ---- config ----
set "ROOT=%~dp0"
set "BACKEND_EXE=%ROOT%yingce-server.exe"
set "WEB_DIR=%ROOT%open-ai-canvas\web"
set "PI_DIR=%ROOT%open-ai-canvas\backend\agent-runtime\pi"
set "PLUGIN_DIR=%ROOT%open-ai-canvas\plugin-packages"
set "DATA_DIR=%ROOT%data"
rem ---- ports: change here only; backend and frontend follow automatically ----
set "API_PORT=8080"
set "WEB_PORT=3000"
rem ---- backend: data dir lives next to the exe, so the install dir is movable ----
set "CANVAS_BACKEND_DATA_DIR=%DATA_DIR%"
set "CANVAS_BACKEND_ADDR=:%API_PORT%"
rem ---- frontend dev proxy target follows API_PORT ----
set "VITE_API_PROXY_TARGET=http://127.0.0.1:%API_PORT%"

rem ---- required env for backend plugin runtime ----
set "CANVAS_OFFICIAL_PLUGIN_DIR=%PLUGIN_DIR%"

rem ---- allow the local Doubao gateway (:9090) as an upstream channel ----
rem Channel creation runs every Base URL through the SSRF guard, which rejects
rem private/loopback addresses. The local gateway lives on 127.0.0.1, so it must
rem be whitelisted explicitly. Only this one host is trusted here; do NOT switch
rem on CANVAS_ALLOW_PRIVATE_UPSTREAMS, which would disable the guard process-wide.
set "CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS=127.0.0.1"

rem ---- server public base URL (required for local storage, else reference media URLs fail) ----
rem When the backend hands a locally uploaded file to a model as a reference image,
rem it must build an absolute URL that the upstream can actually fetch. If unset it
rem reports "server local storage public base URL is not configured".
rem NOTE: MiniMax H3 and other video protocols REQUIRE a public URL; 127.0.0.1 is
rem      rejected upstream (media URL cannot access a private address). Image
rem      protocols inline data URLs, so they are unaffected.
rem So prefer the tunnel address here; fall back to loopback only when no tunnel exists
rem (which is enough for image-only flows). The tunnel URL changes on every restart:
rem edit this line, or override it via publicBaseUrl in Storage Settings.
if exist "%ROOT%bin\cloudflared.exe" (
    set "CANVAS_PUBLIC_BASE_URL=https://lifetime-hampton-validation-labeled.trycloudflare.com"
) else (
    set "CANVAS_PUBLIC_BASE_URL=http://127.0.0.1:%API_PORT%"
)

rem ---- SQLite connection params (no recompile needed; this is what makes them take effect) ----
rem The backend default DSN uses mattn-style _busy_timeout / _journal_mode, but the
rem actual driver is glebarez/go-sqlite, which only understands _pragma=name(value).
rem Result: WAL was never enabled and the DB stayed in journal_mode=delete, so
rem concurrent read/write degraded to mutual exclusion and writes hit
rem database is locked (SQLITE_BUSY). Work saves could not checkpoint and tasks
rem were marked "failed while saving".
rem Here we override with DATABASE_URL using the _pragma form the driver really supports.
rem _txlock=immediate takes the write lock at transaction start, avoiding the
rem SELECT-then-UPDATE pattern in WAL that raises SQLITE_BUSY_SNAPSHOT(517)
rem (busy_timeout does NOT retry that class of error).
rem NOTE: inside set "VAR=...&..." cmd treats & as a command separator and TRUNCATES
rem the variable at the first & (the remainder is executed as a command, which is
rem exactly the "not recognized as an internal or external command" error).
rem The obvious fix, writing ^&, is WRONG here: cmd stops the truncation but leaves
rem a literal ^ inside the stored value, so the driver receives
rem "...busy_timeout(15000)^&_pragma=..." and SQLite dies immediately with
rem   SQL logic error: unrecognized token: "^"
rem and the backend exits before it ever listens (the launcher then hangs forever
rem on the health poll, which is the "slow start" symptom).
rem Correct approach: enable delayed expansion, hold & in its own variable, and
rem splice it in with !AMP! so it is expanded after the line has been parsed.
set "AMP=&"
set "DATABASE_URL=%DATA_DIR%\open_ai_canvas.db?_pragma=busy_timeout(15000)!AMP!_pragma=journal_mode(WAL)!AMP!_pragma=foreign_keys(1)!AMP!_pragma=synchronous(NORMAL)!AMP!_txlock=immediate"

rem ---- ffmpeg for media probe / transcode / agent video inspect ----
if exist "%ROOT%bin" set "PATH=%ROOT%bin;%PATH%"

rem Pin the two system tools we depend on. A PATH that contains Git-for-Windows
rem (or any other POSIX toolchain) can shadow them: bash's `timeout` rejects
rem "/t 1" with "invalid time interval", and bash's `curl` treats "-o NUL" as a
rem real filename instead of the Windows null device. Absolute paths avoid both.
set "CURL_EXE=%SystemRoot%\System32\curl.exe"
if not exist "%CURL_EXE%" set "CURL_EXE=curl.exe"

rem Delay between health polls. `timeout.exe` is deliberately NOT used: whenever
rem stdin is redirected (start /min, CI runners, or any wrapper that pipes the
rem console) it prints "input redirection is not supported, exiting immediately"
rem and returns instantly, turning the poll loop into a busy spin. Pinging the
rem loopback adapter is a side-effect-free way to sleep that ignores stdin.
set "SLEEP_1S=ping -n 2 -w 1000 127.0.0.1 >NUL"

if not exist "%DATA_DIR%" mkdir "%DATA_DIR%"

if not exist "%BACKEND_EXE%" (
    echo [error] %BACKEND_EXE% not found.
    pause
    exit /b 1
)

rem ---- self-heal: agent runtime deps ----
if not exist "%PI_DIR%\node_modules\@earendil-works" (
    echo [setup] agent-runtime deps missing, installing via bun...
    pushd "%PI_DIR%"
    call bun install
    popd
)

rem ---- self-heal: web deps ----
if not exist "%WEB_DIR%\node_modules\vite" (
    echo [setup] web deps missing, installing via bun...
    pushd "%WEB_DIR%"
    call bun install
    popd
)

rem ---- stop old instances ----
taskkill /F /IM yingce-server.exe >nul 2>&1

rem ---- stop stale frontend still holding port 3000 (only the listener on that port) ----
for /f "tokens=5" %%p in ('netstat -ano ^| findstr ":3000 " ^| findstr "LISTENING"') do taskkill /F /PID %%p >nul 2>&1

rem ---- start backend ----
echo [start] backend on port %API_PORT% ...
start "Yingce-Backend" /min cmd /c "cd /d %ROOT% && %BACKEND_EXE% >> %DATA_DIR%\server.log 2>&1"

rem ---- wait for backend health ----
set /a tries=0
:wait_backend
set /a tries+=1
if %tries% gtr 60 (
    echo [error] backend failed to start in 60s. See %DATA_DIR%\server.log
    pause
    exit /b 1
)
"%CURL_EXE%" -s --noproxy "*" -o NUL http://127.0.0.1:%API_PORT%/api/health 2>NUL
if errorlevel 1 (
    %SLEEP_1S%
    goto wait_backend
)
echo [ok] backend healthy

rem ---- start frontend ----
echo [start] frontend on port %WEB_PORT% ...
start "Yingce-Web" /min cmd /c "cd /d %WEB_DIR% && bun run dev >> %DATA_DIR%\web.log 2>&1"

rem ---- wait for frontend ----
set /a tries=0
:wait_web
set /a tries+=1
if %tries% gtr 90 (
    echo [error] frontend failed to start in 90s. See %DATA_DIR%\web.log
    pause
    exit /b 1
)
"%CURL_EXE%" -s --noproxy "*" -o NUL http://127.0.0.1:%WEB_PORT%/ 2>NUL
if errorlevel 1 (
    %SLEEP_1S%
    goto wait_web
)
echo [ok] frontend ready
echo.
echo ================================================
echo   Yingce is running:  http://127.0.0.1:%WEB_PORT%
echo ================================================
echo   To stop: run the stop script  (or close the
echo   two minimized console windows)
echo.
start "" http://127.0.0.1:%WEB_PORT%/
%SLEEP_1S%
endlocal
