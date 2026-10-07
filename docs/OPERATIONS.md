# Mediaphile Server operations

The server is designed for a private LAN, without a Plex account or Internet dependency.

## Build and test

Requires Go 1.24 or newer, and optional `ffprobe`/`ffmpeg` binaries on `PATH` for media scanning and transcoding.

```sh
go test -p 1 ./...
go vet -p 1 ./...
go build -o mediaphile-server ./cmd/mediaphile-server
```

On Windows, use `mediaphile-server.exe`. The service listens on TCP 8097 by default; UDP discovery is on 8098. Set `MEDIAPHILE_HTTP_PORT` and `MEDIAPHILE_DISCOVERY_PORT` to change those.

## Initial local setup

Set `MEDIAPHILE_DATA_DIR` to a writable **local filesystem** location. Never put the SQLite database on an SMB/NFS media share; SQLite requires reliable locking and durable writes.

Set `MEDIAPHILE_TRANSCODE_DIR` to a writable scratch directory on fast local storage with adequate capacity. If FFmpeg is not installed, compatible Direct Play works, but scanning cannot probe files without ffprobe and remux/transcoding are unavailable.

A fresh server emits a one-time administrator bootstrap secret to its local console. Use `POST /api/v1/setup/bootstrap` with `bootstrapSecret`, `username`, `password`. After the first administrator exists the bootstrap endpoint cannot be replayed. Do not post the secret into public bug reports or logs.

Authenticate through `POST /api/v1/auth/login` and send `Authorization: Bearer <session-token>` on protected API calls.

Create media libraries with `POST /api/v1/libraries`. Movie roots should contain `Movie Title (YYYY)` paths. TV roots should contain `Show Name/Season 01/Show Name - S01E01 - Episode Title` paths. `POST /api/v1/libraries/{libraryId}/scan` indexes the files. Scanning never renames or removes source media and leaves ambiguous identities unresolved.

## Networking and isolation

Mediaphile is not a cloud media service. It does not expose a cloud account, tunnel, relay, or public router configuration. Its Go HTTP ingress guard accepts only RFC1918/loopback/link-local IPv4 and IPv6 unique-local/link-local/loopback peers, further narrowed by `MEDIAPHILE_ALLOWED_CIDRS`.

**An application-level remote-IP guard alone is not a firewall.** Docker port publishing, LAN reverse proxies, NAT hairpinning, Tailscale/other relays, and port-forwarding can cause a public request to appear to originate from a local gateway. Configure the host firewall and router so TCP 8097 and UDP 8098 are reachable only from your intended LAN. Do not forward them to the Internet. When running behind a reverse proxy, limit that proxy's source network and configure `MEDIAPHILE_TRUSTED_PROXIES` only for controlled proxies. Never trust Internet-origin forwarded address headers.

The example Compose file binds TCP/UDP ports to 127.0.0.1 **by default**, not the public network. Set `MEDIAPHILE_BIND_IP` to the machine's explicit LAN address only after verifying firewall restrictions.

## Important environment variables

| Variable | Default | Meaning |
| --- | --- | --- |
| `MEDIAPHILE_LISTEN_ADDR` | `0.0.0.0` | Process listening interface |
| `MEDIAPHILE_HTTP_PORT` | `8097` | HTTP API / bundled web UI port |
| `MEDIAPHILE_DISCOVERY_PORT` | `8098` | Local UDP discovery |
| `MEDIAPHILE_DATA_DIR` | `./data` | Local SQLite state |
| `MEDIAPHILE_TRANSCODE_DIR` | `./transcode` | Temporary FFmpeg output |
| `MEDIAPHILE_UI_DIR` | `./ui` | Built Mediaphile Client static bundle |
| `MEDIAPHILE_ALLOWED_CIDRS` | local ranges | Optional narrower client CIDRs; public ranges rejected |
| `MEDIAPHILE_TRUSTED_PROXIES` | none | Explicit controlled reverse-proxy CIDRs |

## Deployment boundaries

The server repo owns its `api/openapi.yaml` schema. The client repo must consume a snapshot pinned to the exact server revision/digest; there is no runtime code import between repos.

The server can serve the client `dist/` bundle from `MEDIAPHILE_UI_DIR` after a compatible client build is available. The server itself does not call any public JavaScript CDN.

The original private `thebrazenbeard/mediaphile` repository remains the knowledge and provenance source. Import only explicitly versioned/digested generated JSON artifacts; the running server must not fetch that repository implicitly.

## Failure guidance

- `FFprobeAvailable: false` in health: install ffprobe on the server, not on the NAS.
- `FFmpegAvailable: false` in health: Direct Play remains possible; remux/transcode unavailable.
- `LAN_ONLY` on a private client: check its actual source address and proxy/docker forwarding before widening CIDRs.
- Empty catalog: create an admin, create a library, run scan, inspect unresolved file names.
- Browser video: the server returns an HttpOnly, SameSite=Strict media cookie alongside successful local login/bootstrap. The cookie authorizes only read-only media/HLS requests. API writes and browsing still require explicit bearer authentication.
