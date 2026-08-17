@echo off
rem Quick start for Windows without docker: builds and runs the server.
rem Needs Go only - the SQLite driver is pure Go, so no C toolchain is required.
rem Double-click this file or run it from cmd / PowerShell.
setlocal
cd /d "%~dp0"

where go >nul 2>nul
if errorlevel 1 (
    echo Go is not installed: https://go.dev/dl/
    pause
    exit /b 1
)

if not exist .env (
    (
        echo # Created by start.bat for a local run. For a server see .env.example.
        echo PORT=8081
        echo DATA_DIR=./data
        echo WEB_DIR=./web
        echo PUBLIC_BASE_URL=http://127.0.0.1:8081
        echo MAX_UPLOAD_MB=4096
        echo.
        echo # Vedrow login is off until these are filled: /auth/vedrow/start answers 503.
        echo # Redirect URI to register in Vedrow: http://127.0.0.1/auth/vedrow/callback
        echo VEDROW_API_URL=
        echo VEDROW_WEB_URL=
        echo VEDROW_CLIENT_ID=
        echo VEDROW_CLIENT_SECRET=
        echo.
        echo # Vedrow usernames or emails allowed to create modpacks, comma separated.
        echo ADMINS=
    ) > .env
    echo created .env for a local run - fill in VEDROW_* and ADMINS to enable login
)

echo building...
go build -o pasyot-launcher.exe .
if errorlevel 1 (
    echo build failed
    pause
    exit /b 1
)

echo.
echo starting - open the address the server prints below, Ctrl+C to stop
echo.
pasyot-launcher.exe
pause
