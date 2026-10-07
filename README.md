# Mediaphile Server

Mediaphile Server is the LAN-native media runtime for the Mediaphile ecosystem, separate from the private provenance/knowledge corpus and the public Mediaphile Client.

The server is written in Go, uses local SQLite and FFmpeg/ffprobe, and exposes the versioned HTTP API at `/api/v1`. It has no Plex account or cloud service dependency.

**Development (Go 1.24+):**

```sh
go test -p 1 ./...
go build ./cmd/mediaphile-server
```

The primary network endpoint defaults to TCP 8097, UDP discovery to 8098. Local users and bearer auth protect API actions. The scanner recognizes movie and TV naming patterns without renaming files. Playback decisions distinguish Direct Play, Remux and Transcode.

See [Operations](docs/OPERATIONS.md), [Design](docs/superpowers/specs/2026-10-07-mediaphile-lan-runtime-design.md), [Implementation Plan](docs/plans/2026-10-07-mediaphile-server-v1.md), and [OpenAPI](api/openapi.yaml).

**Status:** V1 implementation branch; not yet approved for Internet exposure or production deployment. Only mount trusted media folders and protect the host's LAN ports with firewall rules. The application LAN guard does not replace the firewall when a reverse proxy/NAT hides source addresses.
