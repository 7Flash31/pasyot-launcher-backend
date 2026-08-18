#!/bin/sh
# Quick start for macOS and Linux.
#
#   ./start.sh            build and run locally; installs Go if there is none
#   ./start.sh --docker   run the full stack in docker (backend + Caddy)
#
# On the first run it creates .env with this machine's LAN address, so other
# computers on the network can download modpacks from it.
set -e
cd "$(dirname "$0")"

PORT_DEFAULT=8081
TOOLCHAIN=.toolchain

lan_ip() {
	ip=""
	case "$(uname -s)" in
	Darwin)
		iface=$(route -n get default 2>/dev/null | awk '/interface:/ {print $2}')
		[ -n "$iface" ] && ip=$(ipconfig getifaddr "$iface" 2>/dev/null || true)
		[ -z "$ip" ] && ip=$(ipconfig getifaddr en0 2>/dev/null || true)
		;;
	Linux)
		ip=$(ip route get 1.1.1.1 2>/dev/null |
			awk '{for (i = 1; i <= NF; i++) if ($i == "src") print $(i + 1)}' | head -1)
		[ -z "$ip" ] && ip=$(hostname -I 2>/dev/null | awk '{print $1}')
		;;
	esac
	[ -z "$ip" ] && ip=127.0.0.1
	echo "$ip"
}

write_env() {
	ip=$(lan_ip)
	cat > .env <<EOF
# Created by start.sh for a local run. For a server see .env.example.
PORT=$PORT_DEFAULT
DATA_DIR=./data
WEB_DIR=./web

# Address of this machine on the local network: modpack manifests and the
# .pasyotpack file will point here, so other computers can install from it.
# Changes with DHCP — on a real server put a domain here instead.
PUBLIC_BASE_URL=http://$ip:$PORT_DEFAULT
SITE_ADDRESS=:80
MAX_UPLOAD_MB=4096

# Vedrow login is off until VEDROW_* are filled: /auth/vedrow/start answers 503.
# Vedrow only accepts https redirect URIs or loopback ones, never a LAN address,
# so the callback stays on 127.0.0.1 — log in as admin from this machine at
# http://127.0.0.1:$PORT_DEFAULT. Register in Vedrow:
#     http://127.0.0.1/auth/vedrow/callback
VEDROW_REDIRECT_URI=http://127.0.0.1:$PORT_DEFAULT/auth/vedrow/callback
VEDROW_API_URL=
VEDROW_WEB_URL=
VEDROW_CLIENT_ID=
VEDROW_CLIENT_SECRET=

# Vedrow usernames or emails allowed to create modpacks, comma separated.
ADMINS=
EOF
	echo "created .env — this machine is http://$ip:$PORT_DEFAULT"
	echo "fill in VEDROW_* and ADMINS to enable login"
}

install_go() {
	os=$(uname -s | tr 'A-Z' 'a-z')
	case "$(uname -m)" in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) echo "unsupported cpu: $(uname -m) — install Go by hand: https://go.dev/dl/"; exit 1 ;;
	esac

	version=$(curl -fsSL "https://go.dev/VERSION?m=text" | head -1)
	[ -n "$version" ] || { echo "cannot reach go.dev — install Go by hand: https://go.dev/dl/"; exit 1; }
	archive="$version.$os-$arch.tar.gz"

	echo "Go is not installed, downloading $version for $os/$arch (about 80 MB)…"
	mkdir -p "$TOOLCHAIN"
	curl -fL --progress-bar "https://go.dev/dl/$archive" -o "$TOOLCHAIN/$archive"

	if command -v python3 >/dev/null 2>&1; then
		want=$(python3 - "$archive" <<-'PY'
			import json, sys, urllib.request
			name = sys.argv[1]
			data = json.load(urllib.request.urlopen("https://go.dev/dl/?mode=json"))
			for release in data:
			    for f in release.get("files", []):
			        if f.get("filename") == name:
			            print(f.get("sha256", ""))
			            raise SystemExit
		PY
		)
		got=$(shasum -a 256 "$TOOLCHAIN/$archive" 2>/dev/null | cut -d' ' -f1 ||
			sha256sum "$TOOLCHAIN/$archive" | cut -d' ' -f1)
		if [ -n "$want" ] && [ "$want" != "$got" ]; then
			rm -f "$TOOLCHAIN/$archive"
			echo "checksum mismatch, download aborted"
			exit 1
		fi
		[ -n "$want" ] && echo "checksum ok"
	fi

	rm -rf "$TOOLCHAIN/go"
	tar -C "$TOOLCHAIN" -xzf "$TOOLCHAIN/$archive"
	rm -f "$TOOLCHAIN/$archive"
	echo "Go installed into $TOOLCHAIN/go — nothing outside this folder was touched"
}

[ -f .env ] || write_env

if [ "$1" = "--docker" ]; then
	command -v docker >/dev/null || { echo "docker is not installed: https://docs.docker.com/get-docker/"; exit 1; }
	docker compose up -d --build
	echo
	echo "site:  http://$(lan_ip)   (Caddy on port 80, from SITE_ADDRESS)"
	echo "logs:  docker compose logs -f backend"
	echo "stop:  docker compose down"
	echo
	echo "note: PUBLIC_BASE_URL in .env must match the address above, otherwise"
	echo "      manifest URLs and the Vedrow redirect URI will point elsewhere"
	exit 0
fi

if command -v go >/dev/null 2>&1; then
	GO=go
elif [ -x "$TOOLCHAIN/go/bin/go" ]; then
	GO="$TOOLCHAIN/go/bin/go"
else
	install_go
	GO="$TOOLCHAIN/go/bin/go"
fi

PORT=$(sed -n 's/^PORT=\([0-9]*\).*/\1/p' .env | head -1)
[ -n "$PORT" ] || PORT=$PORT_DEFAULT

echo "building…"
"$GO" build -o pasyot-launcher .
echo "this machine:  http://$(lan_ip):$PORT"
echo "admin login:   http://127.0.0.1:$PORT   (Vedrow needs loopback)"
echo "Ctrl+C to stop"
exec ./pasyot-launcher
