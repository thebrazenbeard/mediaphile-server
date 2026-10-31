# Mediaphile Server

**A self-hosted, local-network media catalog and playback API written in Go.**

Mediaphile Server indexes movies and television from directories you choose, stores metadata and per-user viewing state in a local SQLite database, and serves compatible files directly or through optional FFmpeg remux/transcoding. It has no required Plex account, cloud relay, or public metadata service. Applications can consume its versioned [OpenAPI contract](api/openapi.yaml), and a compatible [Mediaphile Client](https://github.com/thebrazenbeard/mediaphile-client) build can be served as the web interface.

The V1 server implementation is **present on `main`**. It is source-level software, not a claim of a production-qualified deployment or independent security certification.

## What it includes

| Area | Implemented surface |
| --- | --- |
| Library/catalog | Movie and TV roots, FFprobe-assisted scans, local SQLite catalog, search, details and ambiguous-file handling |
| Playback | HTTP byte-range Direct Play, capability-based playback decisions, sessions, FFmpeg-backed HLS remux/transcoding when installed |
| Accounts | One-time local administrator bootstrap, password-based login, per-user watch/resume state, scoped Continue Watching |
| Integration | Versioned `/api/v1` endpoints, SSE events, local webhooks, UDP discovery, optional same-origin static client bundle |
| Data boundaries | Separate database and transcode directories, read-only media-root deployment pattern, bounded provenance-import entry point |

The scanner does not rename or delete source media. Clients use catalog IDs and scoped streaming routes rather than learning raw server filesystem paths. Media probing, codec support, browser playback, and NAS compatibility still depend on the actual environment.

## Build and test

The checked-in [`go.mod`](go.mod) declares **Go 1.26.0**. Install [`ffprobe`](https://ffmpeg.org/ffprobe.html) to scan video files and [`ffmpeg`](https://ffmpeg.org/ffmpeg.html) for remux/transcoding. Direct Play of already indexed compatible files can work without FFmpeg.

```sh
git clone https://github.com/thebrazenbeard/mediaphile-server.git
cd mediaphile-server
go mod download
go test -p 1 ./...
go vet -p 1 ./...
go build -o mediaphile-server ./cmd/mediaphile-server
```

Run the executable on the intended private host (`mediaphile-server.exe` on Windows). The default HTTP port is **8097/TCP** and local discovery uses **8098/UDP**. Set environment variables for local writable state and transcode scratch space, for example:

```sh
export MEDIAPHILE_DATA_DIR=./local-state
export MEDIAPHILE_TRANSCODE_DIR=./local-transcodes
./mediaphile-server
```

On the first start, the server prints a **one-time bootstrap secret to its local console**. Submit it to `POST /api/v1/setup/bootstrap` with an administrator username and password, then authenticate through `POST /api/v1/auth/login`. Treat the secret and returned tokens as credentials; never commit or include them in reports. After bootstrap, add explicit Movie and TV library roots and initiate scans through the authenticated API or a compatible client. See [operations](docs/OPERATIONS.md) and the [API schema](api/openapi.yaml) for request details.

For a containerized development start, [`compose.yaml`](compose.yaml) publishes both ports to **`127.0.0.1` by default**, using the `MEDIAPHILE_BIND_IP` override only when configured intentionally. It mounts a media directory read-only and keeps catalog/transcode data in separate volumes. The example is not a turnkey secure Internet deployment.

## How applications use it

| Endpoint or route | Purpose |
| --- | --- |
| `GET /api/v1/health` | Local readiness and availability of media tools |
| `POST /api/v1/libraries` | Add an explicitly authorized movie or TV root |
| `POST /api/v1/libraries/{libraryId}/scan` | Index the selected library |
| `GET /api/v1/users/me/continue-watching?limit=12` | Authenticated user's resumable playable items |
| Media/session endpoints | Authorized Direct Play, range requests and HLS session artifacts |

The [Continue Watching contract](docs/CONTINUE_WATCHING_V1.md) is filtered by authenticated user and playability. Browser media reads use a same-origin, HttpOnly media-session cookie; API management still requires explicit bearer credentials. The full route contract is in [`api/openapi.yaml`](api/openapi.yaml), and client-integration expectations are in the [unified UI requirements](docs/UNIFIED_UI_API_REQUIREMENTS.md).

## Configuration and deployment boundaries

| Variable | Default | Role |
| --- | --- | --- |
| `MEDIAPHILE_LISTEN_ADDR` | `0.0.0.0` | Process bind interface, **not** a guarantee of safe exposure |
| `MEDIAPHILE_HTTP_PORT` | `8097` | HTTP API and optional UI |
| `MEDIAPHILE_DISCOVERY_PORT` | `8098` | UDP LAN discovery |
| `MEDIAPHILE_DATA_DIR` | `./data` | Local SQLite catalog and account state |
| `MEDIAPHILE_TRANSCODE_DIR` | `./transcode` | Local temporary media output |
| `MEDIAPHILE_UI_DIR` | `./ui` | Prebuilt client static assets |
| `MEDIAPHILE_ALLOWED_CIDRS` | unset | Optional additional client-network restriction |
| `MEDIAPHILE_TRUSTED_PROXIES` | unset | Explicitly trusted proxy networks only |

Keep the SQLite database on a **local filesystem**, not SMB/NFS media shares. Mount media roots read-only where practical, and use dedicated storage for temporary transcoding. The HTTP ingress guard restricts remote addresses to local/private ranges, but **an application-level IP check is not a firewall**. Reverse proxies, Docker, VPNs, NAT, and port forwarding can change the effective source address; restrict both TCP 8097 and UDP 8098 at the host and network boundary. This software is designed for private LAN use, not direct Internet exposure. The [operations guide](docs/OPERATIONS.md) describes the network and failure cases.

An optional static web client can be hosted from `MEDIAPHILE_UI_DIR` after building a compatible client bundle. Neither a client build nor a passing test is proof that playback on a particular TV, phone, NAS, or browser has been qualified.

## Verification and release status

Source tests cover catalog/authentication, scanning, playback decisions and sessions, streaming, discovery, events, webhooks, ingress guards, UI static serving, and an integration vertical slice. The [CI workflow](.github/workflows/ci.yml) builds and tests the Go code; this README's commands are the local checks. Independent qualification of live NAS mounts, codecs, network configuration, long-running transcoding, and hardware decoding is still required for a real deployment.

The repository currently has **no tracked root `LICENSE` or `COPYING` file**. Its public visibility is not a license to redistribute, modify commercially, or host the software for others. Bundled font assets under `public/fonts/` also require a separate provenance and rights review before redistribution. Do not treat this repository as an unrestricted reusable open-source package until publication terms are supplied.

The server's local authentication and playback permissions do **not** constitute a general-purpose host-owned authorization service, cryptographic provider-identity binding, or transferable consent across AI models. The private Mediaphile research corpus is not automatically fetched or altered by the server.
