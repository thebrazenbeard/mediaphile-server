# Mediaphile Server

Mediaphile Server is the LAN-only media runtime for the Mediaphile ecosystem.

The V1 architecture is defined in `docs/superpowers/specs/2026-10-07-mediaphile-lan-runtime-design.md` and its execution plan in `docs/plans/2026-10-07-mediaphile-server-v1.md`.

## Development

Requires Go 1.24 or newer.

```sh
go test ./...
go build ./cmd/mediaphile-server
```

The default HTTP listener is port 8097. Requests are accepted only from loopback, RFC1918/private, link-local, and IPv6 unique-local source addresses unless the administrator narrows the allowed CIDRs.

No Plex account, cloud relay, or router configuration is required.
