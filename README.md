# Mediaphile Server

**Your local media. Your network. Your rules.**

Mediaphile Server is the Go-based, local-network-only media server powering the Mediaphile ecosystem. It catalogs Movies and TV Shows from local folders or mounted shares, records local viewing state, and provides Direct Play, remux, and transcoding without a Plex account or cloud relay.

| Repository | Responsibility |
| --- | --- |
| [mediaphile](https://github.com/thebrazenbeard/mediaphile) | Private media research, knowledge, provenance, and governed ingestion |
| **mediaphile-server** | LAN API, SQLite catalog, scanning, playback decisions, media delivery, authentication |
| [mediaphile-client](https://github.com/thebrazenbeard/mediaphile-client) | One consistent interface across phones, tablets, desktop browsers, and large screens |

> **Development status:** The V1 implementation lives on [`build/mediaphile-server-v1`](https://github.com/thebrazenbeard/mediaphile-server/tree/build/mediaphile-server-v1) in [draft PR #1](https://github.com/thebrazenbeard/mediaphile-server/pull/1). This is not a production-qualified release. The main branch is deliberately not merged without approval.

## Implemented architecture

```text
LAN browser / native client
         │
         ▼
LAN ingress guard + local account authentication
         │
         ▼
     /api/v1   ◄── versioned OpenAPI contract
         │
  ┌──────┼──────────────┐
  ▼      ▼              ▼
Catalog  Playback       SSE / local webhooks
SQLite   engine         and discovery
  ▲      │
  │      ├── Direct Play (HTTP range)
  │      └── FFmpeg remux/transcode (HLS)
  │
Scanner + FFprobe ──► Local disk / read-only NAS share
```

The server stores **media metadata and playback state in local SQLite**, not on the media share. It never renames or deletes source media while scanning. Clients receive opaque IDs and authorized stream URLs, never filesystem paths.

Current API includes local bootstrap/login/logout, libraries and scans, browse/search/detail, per-user watch-state filtering, playback decisions and sessions, resume state, byte ranges, HLS artifacts, events/webhooks, discovery, and a bounded provenance-import seam.

## Build and run locally

Requires **Go 1.24+**, `ffprobe` to scan video files, and `ffmpeg` for remux/transcoding.

```sh
go mod download
go test ./...
go vet ./...
go build -o mediaphile-server ./cmd/mediaphile-server
```

Run `./mediaphile-server` (or `mediaphile-server.exe` on Windows). Defaults: TCP **8097** for HTTP and UDP **8098** for LAN discovery.

The server prints a one-time bootstrap secret to its **local console** the first time you run it. Configure a local administrator using the client's setup screen or `POST /api/v1/setup/bootstrap`. The secret must not be published. After creating the administrator, add Movie and TV library roots in Settings, then scan them.

To serve the web client from the same origin, build [mediaphile-client](https://github.com/thebrazenbeard/mediaphile-client) and set `MEDIAPHILE_UI_DIR` to its `dist/` directory. See the [client README](https://github.com/thebrazenbeard/mediaphile-client/tree/build/mediaphile-client-v1#readme) for details.

## Safety and configuration

- Keep the SQLite database in `MEDIAPHILE_DATA_DIR` on a **local filesystem**, not SMB/NFS.
- Mount NAS/media folders read-only wherever possible; create a separate writable `MEDIAPHILE_TRANSCODE_DIR`.
- The HTTP guard rejects public source addresses, but **host firewall and router rules are also required**. Do not expose TCP 8097 or UDP 8098 to the Internet.
- The example [Compose file](compose.yaml) binds to **127.0.0.1 by default**; set an explicit LAN bind address only with appropriate firewall controls.
- Accounts and tokens remain local. Browser media uses a same-origin, HttpOnly media-session cookie, never an access token in the video URL.
- No CDN, external metadata service, cloud account, or remote access is required for runtime operation.

Full settings: [Operations](docs/OPERATIONS.md) · [OpenAPI](api/openapi.yaml) · [Unified UI API contract](docs/UNIFIED_UI_API_REQUIREMENTS.md).

## Verification

```sh
go test ./...
go vet ./...
```

The companion client includes `npm run e2e:real` for an **isolated synthetic-media integration test**. It builds the real Go server, generates a temporary H.264/AAC movie with FFmpeg, scans it, exercises authentication and HTTP range delivery, and verifies actual playback in Chromium. It does not need access to your NAS or existing Plex database.

## Deliberate boundaries

This is a **LAN-only** V1 runtime, not an Internet-facing streaming product. Native TV hardware decoders, Roku/tvOS packaging, production NAS paths, long-running transcoding, and deployment behavior require separate qualification. The private corpus and its provenance rules remain the source of truth for governed knowledge; that does not imply automatic writes or merges to it.
