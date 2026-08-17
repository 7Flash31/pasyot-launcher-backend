# Pasyot launcher backend

A minimal backend: serves the launcher download on the main page, lets an admin
in through Vedrow login and stores modpacks as versions so that the launcher
downloads only what changed.

## Install on a server

Docker is the only requirement. Three commands:

```bash
git clone <repo> pasyot-launcher && cd pasyot-launcher
cp .env.example .env && nano .env      # SITE_ADDRESS, PUBLIC_BASE_URL, VEDROW_*, ADMINS
docker compose up -d --build
```

That is all: Caddy issues an HTTPS certificate for the domain from
`SITE_ADDRESS`, the backend comes up behind it and serves both the site and the
API. No external database, no S3, no migrations.

Update with `git pull && docker compose up -d --build`.

## Quick start on a workstation

The start scripts create a local `.env` on first run (they never overwrite an
existing one) and bring the server up:

```bash
./start.sh              # macOS, Linux: build and run locally, needs Go
./start.sh --docker     # the same stack as on a server, needs docker
```

```bat
start.bat               ::  Windows: build and run locally, needs Go, no docker
```

Only Go is required for the local path — the SQLite driver is pure Go, so there
is no C toolchain to install. Vedrow login stays off (`503`) until `VEDROW_*` and
`ADMINS` are filled in `.env`; everything else works meanwhile.

By hand it is the same two steps:

```bash
cp .env.example .env   # PUBLIC_BASE_URL=http://127.0.0.1:8081
go run .               # site and API on :8081, data in ./data
```

## Moving to another server

All state is a single `./data` directory (SQLite database + modpack files), so
moving hosts is copying a directory. `./data` is a bind mount, not a named
volume, exactly for this reason.

```bash
# on the old server: stop first so the database closes cleanly (WAL flushed)
docker compose down
rsync -avz data/ new-server:/opt/pasyot-launcher/data/

# on the new one: same clone, same .env
docker compose up -d --build
```

Then point the domain's A record at the new IP. Nothing needs to change in
`.env` or in the Vedrow admin panel — provided `PUBLIC_BASE_URL` is a **domain,
not an IP**: the redirect URI that Vedrow compares exactly then stays the same
across moves.

Order that avoids downtime: bring the new server up with the data already
copied, switch DNS, keep the old one running until the TTL expires.

**About TTL.** An A record change applies at the DNS provider within seconds,
but clients keep the old IP until the TTL expires. So lower the TTL to 60-300
seconds a day before the move and restore it afterwards.

**If the domain is behind Cloudflare** (orange cloud): the IP visible from
outside is Cloudflare's, so changing the server IP takes effect immediately with
no propagation at all, and the server IP stays hidden. One catch: Caddy issues
certificates over HTTP-01, which requires the domain to point at the new server
directly. The simplest path is to grey-cloud the record (proxy off) while the
certificate is issued, then turn the proxy back on (Full strict mode).

## The site

Two static files in `web/`, served by the backend itself (`WEB_DIR`, `./web` by
default):

- `index.html` — the launcher download button, Vedrow login on the left, and a
  "make a modpack" link for an admin;
- `admin.html` — create a modpack and upload an archive; the response contains a
  link to the `.pasyotpack` file.

The site's visible text is Russian on purpose: that is what players see.

One origin for the site and the API, which is why the session cookie just works
and CORS is off by default. If the frontend ever moves to a separate domain, set
`PUBLIC_WEB_URL` (where to return after login) and `CORS_ORIGIN`; without them
the post-login redirect is relative.

## Where the launcher binary comes from

Three sources, the first one set wins:

| source | how | what `/launcher/download` does |
|---|---|---|
| external address | `LAUNCHER_URL` | **302** to it (GitHub release, file hosting) |
| file on disk | `LAUNCHER_FILE` | serves the file itself; in docker drop it into `./data` and point at `/data/<name>` |
| uploaded build | `POST /launcher/builds` | serves it from the store |

`LAUNCHER_VERSION` is shown next to the button. While either env source is set,
uploads answer 409 — otherwise it would be unclear what actually downloads. The
public address stays `/launcher/download` in all three cases, so the button never
has to change.

## How it works

Files are stored **by content**: the path in the store is the file's sha256.
Everything else follows from that:

- a new version where three mods changed takes the space of three mods;
- the launcher compares hashes and downloads only the files that differ;
- `/objects/<sha>` is cacheable forever — that address never holds another file.

A version is immutable. Every archive upload is a new version; old ones stay
installable, so "roll back to the previous build" is just another number in a
manifest. Uploading the same archive twice does **not** create a version (409).

The admin path end to end:

```
Vedrow login -> POST /modpacks -> POST /modpacks/{slug}/versions (zip)
   -> pack_url in the response -> download .pasyotpack -> hand it out
```

The path of a person with the launcher:

```
.pasyotpack (which modpack + which version + manifest_url)
   -> GET manifest -> compare hashes of local files
   -> fetch the mismatching ones from /objects/<sha> -> play
```

A `.pasyotpack` is a few lines of JSON, not the build itself: the launcher takes
the full file list from `manifest_url` and always sees current addresses.

## Vedrow login

Vedrow is an identity provider (see `vedrow-backend/API.md`). We are a
**confidential** client: we present both the secret and PKCE.

How to connect:

1. In the Vedrow admin panel, under Vedrow login: create an application,
   **not public**.
2. The redirect URI is exactly `PUBLIC_BASE_URL/auth/vedrow/callback`. Vedrow
   compares it exactly, no prefixes.
3. `client_id` and the secret go into `.env` (`VEDROW_CLIENT_ID`,
   `VEDROW_CLIENT_SECRET`). Vedrow shows the secret once.
4. `VEDROW_API_URL` is Vedrow's API (`…/api`), `VEDROW_WEB_URL` is its frontend.
   Two addresses is not an oversight: Vedrow's consent screen lives on its
   frontend, while token and userinfo live on the API.

**Who is an admin.** Vedrow does not report a role, and should not: admin of
Pasyot is our role. The `ADMINS` list (usernames or emails, comma separated)
raises the flag on login. A flag already set in the database is never cleared by
`ADMINS` — rights can also be granted by hand.

We do not parse the `id_token`: the code is exchanged for a token directly with
the token endpoint over TLS and the profile comes from `/oauth/userinfo`. Hence
no jwt and no jwks in the dependencies — there is no signature to verify in this
flow.

**Session.** A random token; only its sha256 is stored. It travels in the
`pasyot_session` cookie (HttpOnly, SameSite=Lax) for the site and in
`Authorization: Bearer` for the launcher. Lives 30 days.

## API

The full reference is in `API.md` (written in Russian): every endpoint with
examples, the manifest format and a "how to connect the launcher" section
(install and update algorithm). That file is not in git — a reference goes stale
faster than it gets updated.

Short version of what lives where:

| | |
|---|---|
| `GET /launcher/latest`, `GET /launcher/download` | the download button on the main page |
| `GET /auth/vedrow/start`, `/auth/me`, `POST /auth/logout` | Vedrow login |
| `GET /modpacks`, `/modpacks/{slug}` | list and one modpack |
| `GET /modpacks/{slug}/manifest` | what the launcher downloads: files, hashes, URLs |
| `GET /modpacks/{slug}/pack` | `.pasyotpack` — the file a person carries to the launcher |
| `GET /objects/{sha}` | the files themselves; cached forever, `Range` and `ETag` |
| `POST /modpacks`, `POST /modpacks/{slug}/versions` · admin | create a modpack, upload a version |

Errors are plain text via `http.Error`, the same as in Vedrow: they cannot be
parsed as JSON.

## What is deliberately missing

Left for later on purpose:

- **garbage collection** in the store: deleted modpacks and versions do not free
  files. A "show orphans / purge" endpoint, like Vedrow has;
- **login from the launcher itself**: login currently assumes a browser. A
  desktop client needs a loopback redirect (RFC 8252) — Vedrow supports it, the
  port is not compared for loopback addresses;
- **editing a modpack** (rename, change description) and deleting a single
  version;
- **migrations**: the schema is applied whole and idempotently from
  `internal/store/schema.sql`. The first migration will be needed once the
  database holds data that cannot be lost;
- **rate limits**: Vedrow has them; here the traffic is an admin and launchers,
  so there are none.

## Layout

```
start.sh / start.bat      quick local start; --docker runs the full stack
API.md                    API reference + how to connect the launcher (Russian)
Dockerfile                static binary + web/ on alpine, 40 MB
docker-compose.yml        backend + Caddy; all state in ./data on the host
Caddyfile                 automatic HTTPS, proxies everything to the backend
web/                      the site: index.html + admin.html, nothing else
main.go                   config from the environment, CORS, graceful shutdown
internal/domain           types that go out as JSON
internal/store            SQLite: all database work, no SQL leaks into handlers
internal/blob             content-addressed file store (sha256)
internal/pack             zip -> file list + fingerprint of the set
internal/vedrow           OIDC client: authorize -> token -> userinfo
internal/slug             cyrillic modpack name -> latin slug
internal/handler          routes, middleware, handlers
```

Tests cover the riskiest parts: archive parsing (`internal/pack`),
transliteration and the open-redirect guard.

```bash
go test ./...
```
