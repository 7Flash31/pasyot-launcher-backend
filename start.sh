#!/bin/sh
# Quick start for macOS and Linux.
#
#   ./start.sh            build and run locally (needs Go, no docker)
#   ./start.sh --docker   run the full stack in docker (backend + Caddy)
#
# Creates a local .env on first run if there is none.
set -e
cd "$(dirname "$0")"

PORT_DEFAULT=8081

write_env() {
	cat > .env <<EOF
# Created by start.sh for a local run. For a server see .env.example.
PORT=$PORT_DEFAULT
DATA_DIR=./data
WEB_DIR=./web
PUBLIC_BASE_URL=http://127.0.0.1:$PORT_DEFAULT
SITE_ADDRESS=:80
MAX_UPLOAD_MB=4096

# Vedrow login is off until these are filled: /auth/vedrow/start answers 503.
# Redirect URI to register in Vedrow: http://127.0.0.1/auth/vedrow/callback
VEDROW_API_URL=
VEDROW_WEB_URL=
VEDROW_CLIENT_ID=
VEDROW_CLIENT_SECRET=

# Vedrow usernames or emails allowed to create modpacks, comma separated.
ADMINS=
EOF
	echo "created .env for a local run — fill in VEDROW_* and ADMINS to enable login"
}

[ -f .env ] || write_env

if [ "$1" = "--docker" ]; then
	command -v docker >/dev/null || { echo "docker is not installed: https://docs.docker.com/get-docker/"; exit 1; }
	docker compose up -d --build
	echo
	echo "site:  http://127.0.0.1        (Caddy on port 80, from SITE_ADDRESS)"
	echo "logs:  docker compose logs -f backend"
	echo "stop:  docker compose down"
	echo
	echo "note: PUBLIC_BASE_URL in .env must match the address above, otherwise"
	echo "      manifest URLs and the Vedrow redirect URI will point elsewhere"
	exit 0
fi

command -v go >/dev/null || {
	echo "Go is not installed: https://go.dev/dl/"
	echo "(or run the stack in docker: ./start.sh --docker)"
	exit 1
}

PORT=$(sed -n 's/^PORT=\([0-9]*\).*/\1/p' .env | head -1)
[ -n "$PORT" ] || PORT=$PORT_DEFAULT

echo "building…"
go build -o pasyot-launcher .
echo "site: http://127.0.0.1:$PORT   (Ctrl+C to stop)"
exec ./pasyot-launcher
