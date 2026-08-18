@echo off
rem Quick start for Windows without docker: builds and runs the server.
rem If there is no Go, install-go.ps1 downloads it into .toolchain\go.
rem Double-click this file or run it from cmd / PowerShell.
setlocal enabledelayedexpansion
cd /d "%~dp0"

set GO=go
where go >nul 2>nul
if errorlevel 1 (
    if exist ".toolchain\go\bin\go.exe" (
        set GO=.toolchain\go\bin\go.exe
    ) else (
        powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0install-go.ps1"
        if errorlevel 1 (
            echo Could not install Go. Install it by hand: https://go.dev/dl/
            pause
            exit /b 1
        )
        set GO=.toolchain\go\bin\go.exe
    )
)

if not exist .env (
    set IP=127.0.0.1
    for /f "usebackq delims=" %%i in (`powershell -NoProfile -Command "(Get-NetIPConfiguration ^| Where-Object {$_.IPv4DefaultGateway -ne $null} ^| Select-Object -First 1).IPv4Address.IPAddress"`) do set IP=%%i
    if "!IP!"=="" set IP=127.0.0.1

    (
        echo # Created by start.bat for a local run. For a server see .env.example.
        echo PORT=8081
        echo DATA_DIR=./data
        echo WEB_DIR=./web
        echo.
        echo # Address of this machine on the local network: modpack manifests and the
        echo # .pasyotpack file will point here, so other computers can install from it.
        echo # Changes with DHCP - on a real server put a domain here instead.
        echo PUBLIC_BASE_URL=http://!IP!:8081
        echo MAX_UPLOAD_MB=4096
        echo.
        echo # Vedrow login is off until VEDROW_* are filled: /auth/vedrow/start answers 503.
        echo # Vedrow only accepts https redirect URIs or loopback ones, never a LAN
        echo # address, so the callback stays on 127.0.0.1 - log in as admin from this
        echo # machine at http://127.0.0.1:8081. Register in Vedrow:
        echo #     http://127.0.0.1/auth/vedrow/callback
        echo VEDROW_REDIRECT_URI=http://127.0.0.1:8081/auth/vedrow/callback
        echo VEDROW_API_URL=
        echo VEDROW_WEB_URL=
        echo VEDROW_CLIENT_ID=
        echo VEDROW_CLIENT_SECRET=
        echo.
        echo # Vedrow usernames or emails allowed to create modpacks, comma separated.
        echo ADMINS=
    ) > .env
    echo created .env - this machine is http://!IP!:8081
    echo fill in VEDROW_* and ADMINS to enable login
)

echo building...
"%GO%" build -o pasyot-launcher.exe .
if errorlevel 1 (
    echo build failed
    pause
    exit /b 1
)

echo.
echo admin login: http://127.0.0.1:8081   ^(Vedrow needs loopback^)
echo Ctrl+C to stop
echo.
pasyot-launcher.exe
pause
