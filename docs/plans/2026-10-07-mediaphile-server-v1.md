# Mediaphile Server V1 Implementation Plan

> **For agentic workers:** Use the host's available task-by-task implementation workflow. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the first production-capable LAN-only Mediaphile Server that scans local movie/TV libraries, persists a catalog, authenticates local users, serves a versioned API, makes deterministic playback decisions, Direct Plays compatible files, and remuxes/transcodes incompatible files through FFmpeg.

**Architecture:** One Go process owns the LAN boundary, local authentication, SQLite catalog, scanner/probe pipeline, playback/session state, event delivery, and media serving. `api/openapi.yaml` is the machine-readable cross-repo contract. Media transformation is isolated behind typed FFmpeg interfaces; the web client never sees filesystem paths or database details.

**Tech Stack:** Go 1.24+, `net/http`, `modernc.org/sqlite`, `golang.org/x/crypto/argon2`, OpenAPI 3.1 YAML, FFmpeg/ffprobe, Go `testing` + `httptest`.

## Global Constraints

- Server ingress is LAN/loopback/link-local only by default.
- No Plex account, Plex protocol emulation, router manipulation, cloud relay, or public discovery.
- Local network location never grants administrator authority.
- Server remains usable with the Internet unavailable.
- Direct Play is preferred over Remux, which is preferred over Transcode.
- Logical items are separate from media sources, parts, and streams.
- Ambiguous identity is preserved unresolved rather than guessed.
- Clients consume only the versioned API and server URLs, never raw filesystem paths or SQLite.
- The private `thebrazenbeard/mediaphile` repo remains the provenance/knowledge authority.
- Plex GPL code is behavioral research only and must not be copied into this repository.
- No implementation license is selected by this plan.
- No deployment, router change, credential change, or destructive cleanup is part of implementation.

---

### Task 1: Establish the Go service baseline, LAN guard, and health surface

**Files:**
- Create: `go.mod`
- Create: `cmd/mediaphile-server/main.go`
- Create: `internal/config/config.go`
- Create: `internal/netguard/netguard.go`
- Create: `internal/netguard/netguard_test.go`
- Create: `internal/httpapi/router.go`
- Create: `internal/httpapi/health.go`
- Create: `internal/httpapi/health_test.go`
- Create: `README.md`
- Create: `.gitignore`

**Interfaces:**
- Produces: `config.Config` with listen address, HTTP port, UDP discovery port, data directory, transcode directory, trusted proxies, and allowed LAN CIDRs.
- Produces: `netguard.Middleware(next http.Handler) http.Handler`.
- Produces: `GET /api/v1/health` returning JSON liveness/readiness/capability fields.

- [ ] **Step 1: Add the focused failing tests**

Add table-driven tests proving loopback, RFC1918 IPv4, IPv4 link-local, IPv6 loopback/link-local/ULA are accepted and public addresses are rejected. Add tests proving an untrusted caller cannot bypass the decision with `X-Forwarded-For`, while a configured trusted proxy may supply a private forwarded source.

Add an HTTP test proving `GET /api/v1/health` behind the guard returns `200` to an accepted source and `403` with stable code `LAN_ONLY` to a public source.

- [ ] **Step 2: Verify the relevant failure**

Run: `go test ./internal/netguard ./internal/httpapi`  
Expected: non-zero exit because the packages/behavior do not yet exist.

- [ ] **Step 3: Implement the minimum behavior**

Use `net/netip` prefixes rather than string-prefix matching. Derive the effective source address from `RemoteAddr`; consult forwarded headers only when the immediate peer is in the explicit trusted-proxy set. Return JSON errors as `{"error":{"code":"LAN_ONLY","message":"..."}}`.

Use a standard-library `http.ServeMux`; no external router is required for the initial API.

- [ ] **Step 4: Verify the focused pass**

Run: `go test ./internal/netguard ./internal/httpapi`  
Expected: zero exit; all guard/health tests pass.

- [ ] **Step 5: Run the affected integration check**

Run: `go test ./...` and `go build ./cmd/mediaphile-server`  
Expected: zero exit and a buildable server binary.

- [ ] **Step 6: Commit the passing deliverable**

```bash
git add go.mod cmd internal README.md .gitignore
git commit -m "feat: establish LAN-only server baseline"
```

### Task 2: Add SQLite migrations and the catalog repository

**Files:**
- Create: `internal/catalog/db.go`
- Create: `internal/catalog/models.go`
- Create: `internal/catalog/repository.go`
- Create: `internal/catalog/repository_test.go`
- Create: `migrations/001_initial.sql`
- Create: `internal/catalog/migrate.go`
- Create: `internal/catalog/migrate_test.go`

**Interfaces:**
- Consumes: `config.Config.DataDir`.
- Produces: `catalog.Open(path string) (*sql.DB, error)`.
- Produces repositories for libraries, items, editions, media sources, media parts, media streams, principals, sessions, playback state, and webhook subscriptions.

- [ ] **Step 1: Add the focused failing tests**

Test migration from an empty temporary database, second-run idempotence, foreign-key enforcement, movie/show/season/episode hierarchy, separate source/part/stream persistence, unavailable-part marking without deleting playback state, and transaction rollback on invalid child references.

- [ ] **Step 2: Verify the relevant failure**

Run: `go test ./internal/catalog`  
Expected: non-zero exit because the catalog package is absent.

- [ ] **Step 3: Implement the minimum behavior**

Use `modernc.org/sqlite`. Enable foreign keys on every connection. Store schema version in a migration table. Keep opaque string IDs generated by the application, with stable parent/child relations and uniqueness constraints for the deterministic keys used by scanning.

Do not store bearer tokens or password plaintext in catalog tables.

- [ ] **Step 4: Verify the focused pass**

Run: `go test ./internal/catalog`  
Expected: zero exit; migration and repository behavior pass.

- [ ] **Step 5: Run the affected integration check**

Run: `go test ./...`  
Expected: all current tests pass.

- [ ] **Step 6: Commit the passing deliverable**

```bash
git add internal/catalog migrations
git commit -m "feat: add durable media catalog"
```

### Task 3: Implement deterministic scanning, identity parsing, and ffprobe normalization

**Files:**
- Create: `internal/probe/probe.go`
- Create: `internal/probe/ffprobe.go`
- Create: `internal/probe/ffprobe_test.go`
- Create: `internal/identity/parser.go`
- Create: `internal/identity/parser_test.go`
- Create: `internal/library/scanner.go`
- Create: `internal/library/scanner_test.go`
- Create: `testdata/probe/movie.json`
- Create: `testdata/probe/episode.json`

**Interfaces:**
- Produces: `probe.Probe(ctx context.Context, path string) (probe.MediaInfo, error)`.
- Produces: `identity.Parse(relativePath string, mediaType catalog.LibraryType) identity.Candidate`.
- Produces: `library.Scanner.Scan(ctx context.Context, libraryID string) (library.ScanResult, error)`.

- [ ] **Step 1: Add the focused failing tests**

Test movie parsing for `Title (2024).mkv`; episode parsing for `Show Name/Season 02/Show Name - S02E03 - Episode Title.mkv`; ambiguous paths remain unresolved; ffprobe JSON normalizes video/audio/subtitle facts; unchanged files are not reprobed; changed size/mtime triggers reprobe; missing files become unavailable; scanner never renames or deletes source files; a symlink escaping the configured root is excluded.

- [ ] **Step 2: Verify the relevant failure**

Run: `go test ./internal/probe ./internal/identity ./internal/library`  
Expected: non-zero exit for missing implementation.

- [ ] **Step 3: Implement the minimum behavior**

Make `ffprobe` an `exec.CommandContext` argument-vector invocation using `-show_format -show_streams -of json`. The scanner walks only the configured root, tracks regular supported media files, compares deterministic file observations, and writes catalog changes transactionally.

Identity parsing produces candidates plus confidence/reason metadata; it does not fabricate a match when required tokens are absent or contradictory.

- [ ] **Step 4: Verify the focused pass**

Run: `go test ./internal/probe ./internal/identity ./internal/library`  
Expected: zero exit.

- [ ] **Step 5: Run the affected integration check**

Run: `go test ./...`  
Expected: all current tests pass.

- [ ] **Step 6: Commit the passing deliverable**

```bash
git add internal/probe internal/identity internal/library testdata/probe
git commit -m "feat: scan and probe media libraries"
```

### Task 4: Publish the OpenAPI V1 contract and browse/detail API

**Files:**
- Create: `api/openapi.yaml`
- Create: `internal/httpapi/errors.go`
- Create: `internal/httpapi/libraries.go`
- Create: `internal/httpapi/items.go`
- Create: `internal/httpapi/browse_test.go`
- Create: `internal/httpapi/openapi_test.go`

**Interfaces:**
- Produces: authoritative `api/openapi.yaml`.
- Produces: `GET /api/v1/server`, `GET /api/v1/libraries`, `GET /api/v1/items`, and `GET /api/v1/items/{itemId}`.
- Produces: opaque cursor pagination and stable error envelope.

- [ ] **Step 1: Add the focused failing tests**

Test movie listing, show/season/episode hierarchy, search query filtering, opaque next-cursor behavior, missing item `404 NOT_FOUND`, no filesystem path leakage in serialized JSON, and an OpenAPI validation test that parses the document and requires every implemented public route/method.

- [ ] **Step 2: Verify the relevant failure**

Run: `go test ./internal/httpapi`  
Expected: failing tests for missing routes/contract.

- [ ] **Step 3: Implement the minimum behavior**

Define explicit response DTOs separate from database models. Include media technical facts and provenance placeholders only as defined fields; do not expose absolute paths. Cursor values are opaque base64url encodings of stable sort keys and are rejected with `400 INVALID_CURSOR` when malformed.

- [ ] **Step 4: Verify the focused pass**

Run: `go test ./internal/httpapi`  
Expected: zero exit.

- [ ] **Step 5: Run the affected integration check**

Run: `go test ./...`  
Expected: all tests pass and the OpenAPI document validates.

- [ ] **Step 6: Commit the passing deliverable**

```bash
git add api internal/httpapi
git commit -m "feat: publish media browse API"
```

### Task 5: Add local bootstrap, password authentication, and revocable sessions

**Files:**
- Create: `internal/auth/password.go`
- Create: `internal/auth/session.go`
- Create: `internal/auth/auth_test.go`
- Create: `internal/httpapi/auth.go`
- Create: `internal/httpapi/auth_test.go`
- Modify: `internal/httpapi/router.go`
- Modify: `cmd/mediaphile-server/main.go`

**Interfaces:**
- Produces: one-time bootstrap secret workflow.
- Produces: `POST /api/v1/setup/bootstrap`, `POST /api/v1/auth/login`, `POST /api/v1/auth/logout`.
- Produces: request principal context used by protected handlers.

- [ ] **Step 1: Add the focused failing tests**

Test bootstrap succeeds only when no administrator exists, bootstrap replay fails, wrong bootstrap secret fails, Argon2id password verification succeeds/fails correctly, login returns an opaque token, stored session value is a hash rather than the bearer token, logout revokes immediately, and protected library administration rejects missing/non-admin principals.

- [ ] **Step 2: Verify the relevant failure**

Run: `go test ./internal/auth ./internal/httpapi -run 'Auth|Bootstrap|Session'`  
Expected: non-zero exit for missing behavior.

- [ ] **Step 3: Implement the minimum behavior**

Use Argon2id with stored per-password salt and encoded parameters. Generate bootstrap/session secrets with `crypto/rand`. Store token SHA-256 hashes for lookup/revocation. Bootstrap secret is printed only to the local process console on first uninitialized start and never logged again after successful setup.

Use `Authorization: Bearer <token>` for API authentication in v1.

- [ ] **Step 4: Verify the focused pass**

Run: `go test ./internal/auth ./internal/httpapi -run 'Auth|Bootstrap|Session'`  
Expected: zero exit.

- [ ] **Step 5: Run the affected integration check**

Run: `go test ./...`  
Expected: all tests pass.

- [ ] **Step 6: Commit the passing deliverable**

```bash
git add internal/auth internal/httpapi cmd/mediaphile-server
git commit -m "feat: add local authentication"
```

### Task 6: Implement client capabilities and deterministic playback decisions

**Files:**
- Create: `internal/playback/capabilities.go`
- Create: `internal/playback/decision.go`
- Create: `internal/playback/decision_test.go`
- Create: `internal/httpapi/playback_decide.go`
- Create: `internal/httpapi/playback_decide_test.go`
- Modify: `api/openapi.yaml`

**Interfaces:**
- Consumes: item media sources/streams, client capability profile, selected streams, quality ceiling, FFmpeg capability.
- Produces: `DIRECT_PLAY | REMUX | TRANSCODE | UNPLAYABLE` plus stable reason codes, chosen source/streams, and a server URL template.

- [ ] **Step 1: Add the focused failing tests**

Test Direct Play for compatible MP4/H.264/AAC; Remux for compatible codecs in unsupported container; Transcode for unsupported video codec; Transcode for subtitle burn-in; quality ceiling forcing transform; inaccessible source exclusion; deterministic source choice between multiple encodes; Unplayable when conversion is required but FFmpeg is unavailable.

- [ ] **Step 2: Verify the relevant failure**

Run: `go test ./internal/playback ./internal/httpapi -run 'Decision|Playback'`  
Expected: non-zero exit.

- [ ] **Step 3: Implement the minimum behavior**

Keep the engine pure: normalized inputs in, immutable decision out. Rank candidates by no-transform first, then compatible quality, then least conversion cost. Return explicit reason codes such as `CONTAINER_UNSUPPORTED`, `VIDEO_CODEC_UNSUPPORTED`, `SUBTITLE_BURN_REQUIRED`, `BITRATE_LIMIT`, and `NO_TRANSFORM_CAPABILITY`.

- [ ] **Step 4: Verify the focused pass**

Run: `go test ./internal/playback ./internal/httpapi -run 'Decision|Playback'`  
Expected: zero exit.

- [ ] **Step 5: Run the affected integration check**

Run: `go test ./...`  
Expected: all tests pass; OpenAPI includes the decision request/response schema.

- [ ] **Step 6: Commit the passing deliverable**

```bash
git add internal/playback internal/httpapi api/openapi.yaml
git commit -m "feat: add playback decision engine"
```

### Task 7: Add secure HTTP byte-range Direct Play

**Files:**
- Create: `internal/stream/direct.go`
- Create: `internal/stream/direct_test.go`
- Create: `internal/httpapi/media.go`
- Create: `internal/httpapi/media_test.go`
- Modify: `api/openapi.yaml`

**Interfaces:**
- Produces: `GET|HEAD /api/v1/media/{partId}/content`.
- Consumes: authenticated principal plus catalog part ID; resolves server-side path internally.

- [ ] **Step 1: Add the focused failing tests**

Test full GET, HEAD, first/open-ended/suffix single ranges, `206 Content-Range`, `416` unsatisfiable range, byte-exact body, unavailable part `404 MEDIA_UNAVAILABLE`, invalid part ID does not become a filesystem path, and traversal strings never escape catalog lookup.

- [ ] **Step 2: Verify the relevant failure**

Run: `go test ./internal/stream ./internal/httpapi -run 'Range|MediaContent'`  
Expected: non-zero exit.

- [ ] **Step 3: Implement the minimum behavior**

Resolve the part from the catalog, open the stored path internally, and use standard-library HTTP range serving semantics. Set content type from normalized catalog media facts with safe fallback `application/octet-stream`.

- [ ] **Step 4: Verify the focused pass**

Run: `go test ./internal/stream ./internal/httpapi -run 'Range|MediaContent'`  
Expected: zero exit.

- [ ] **Step 5: Run the affected integration check**

Create a temporary media file in a test library and exercise catalog part -> HTTP range response through `httptest.Server`.  
Run: `go test ./...`  
Expected: all tests pass.

- [ ] **Step 6: Commit the passing deliverable**

```bash
git add internal/stream internal/httpapi api/openapi.yaml
git commit -m "feat: stream direct-play media"
```

### Task 8: Add playback sessions, progress, resume, and local events

**Files:**
- Create: `internal/playback/session.go`
- Create: `internal/playback/session_test.go`
- Create: `internal/events/bus.go`
- Create: `internal/events/bus_test.go`
- Create: `internal/httpapi/sessions.go`
- Create: `internal/httpapi/sessions_test.go`
- Modify: `api/openapi.yaml`

**Interfaces:**
- Produces session create/update/delete endpoints.
- Produces `GET|PUT /api/v1/users/me/playback/{itemId}`.
- Produces typed events `media.play|resume|pause|progress|stop|completed`.

- [ ] **Step 1: Add the focused failing tests**

Test per-principal resume isolation, bounded progress updates, completion threshold behavior, explicit stop reason, session ownership enforcement, restart persistence through SQLite, and event subscribers receiving typed state changes without blocking the HTTP response.

- [ ] **Step 2: Verify the relevant failure**

Run: `go test ./internal/playback ./internal/events ./internal/httpapi -run 'Session|Progress|Event'`  
Expected: non-zero exit.

- [ ] **Step 3: Implement the minimum behavior**

Persist session state transactionally. Emit events after successful persistence. Event delivery to subscribers uses bounded buffered channels and drops/records slow local subscribers rather than blocking playback.

- [ ] **Step 4: Verify the focused pass**

Run: `go test ./internal/playback ./internal/events ./internal/httpapi -run 'Session|Progress|Event'`  
Expected: zero exit.

- [ ] **Step 5: Run the affected integration check**

Run: `go test ./...`  
Expected: all tests pass.

- [ ] **Step 6: Commit the passing deliverable**

```bash
git add internal/playback internal/events internal/httpapi api/openapi.yaml
git commit -m "feat: persist playback sessions"
```

### Task 9: Add FFmpeg remux/transcode session management

**Files:**
- Create: `internal/transcode/command.go`
- Create: `internal/transcode/manager.go`
- Create: `internal/transcode/command_test.go`
- Create: `internal/transcode/manager_test.go`
- Create: `internal/httpapi/transcode.go`
- Create: `internal/httpapi/transcode_test.go`
- Modify: `internal/httpapi/playback_decide.go`
- Modify: `api/openapi.yaml`

**Interfaces:**
- Produces typed FFmpeg argument vectors for Remux/HLS Transcode.
- Produces session-scoped HLS playlist/segment endpoints.
- Produces process cancellation/expiry behavior tied to playback sessions.

- [ ] **Step 1: Add the focused failing tests**

Test that hostile filenames remain single argv entries; Remux copies compatible elementary streams; Transcode encodes only incompatible streams; subtitle burn-in adds an explicit filter argument; output stays beneath the configured transcode root; session stop cancels its process; expiry removes only that session directory; HLS path traversal is rejected.

- [ ] **Step 2: Verify the relevant failure**

Run: `go test ./internal/transcode ./internal/httpapi -run 'Transcode|Remux|HLS'`  
Expected: non-zero exit.

- [ ] **Step 3: Implement the minimum behavior**

Never invoke a shell. Use `exec.CommandContext(binary, args...)`. Write each transcode under `<transcode-root>/<session-id>/`. Serve generated artifacts only after cleaning and verifying the relative requested path remains inside that directory.

Real FFmpeg execution tests are tagged/integration-gated; unit tests use a process-runner seam.

- [ ] **Step 4: Verify the focused pass**

Run: `go test ./internal/transcode ./internal/httpapi -run 'Transcode|Remux|HLS'`  
Expected: zero exit.

- [ ] **Step 5: Run the affected integration check**

Run: `go test ./...`  
If FFmpeg is installed, additionally run: `go test -tags=ffmpeg ./internal/transcode`  
Expected: base suite passes regardless of FFmpeg presence; tagged suite passes when capability exists.

- [ ] **Step 6: Commit the passing deliverable**

```bash
git add internal/transcode internal/httpapi api/openapi.yaml
git commit -m "feat: remux and transcode playback"
```

### Task 10: Add webhooks, discovery, provenance import seam, client hosting, and end-to-end qualification

**Files:**
- Create: `internal/events/webhook.go`
- Create: `internal/events/webhook_test.go`
- Create: `internal/discovery/discovery.go`
- Create: `internal/discovery/discovery_test.go`
- Create: `internal/provenance/import.go`
- Create: `internal/provenance/import_test.go`
- Create: `internal/ui/static.go`
- Create: `internal/ui/static_test.go`
- Create: `internal/httpapi/webhooks.go`
- Create: `internal/httpapi/webhooks_test.go`
- Create: `internal/integration/vertical_slice_test.go`
- Create: `docs/OPERATIONS.md`
- Create: `Dockerfile`
- Create: `compose.yaml`
- Modify: `cmd/mediaphile-server/main.go`
- Modify: `api/openapi.yaml`
- Modify: `README.md`

**Interfaces:**
- Produces signed LAN webhook delivery.
- Produces Mediaphile Discovery V1 on configurable UDP port.
- Produces a provenance import boundary carrying source revision/digest and evidence class.
- Produces static client hosting at `/`.
- Produces the first end-to-end scan -> browse -> decide -> range-stream proof.

- [ ] **Step 1: Add the focused failing tests**

Test webhook targets reject public addresses by default, delivery signature is reproducible/verifiable, retry is bounded and cannot fail playback, discovery response contains server ID/name/port/API version without credentials, provenance import preserves evidence class and conflict state, static assets use local files only, and the vertical slice scans a deterministic fake-probed file then browses, decides Direct Play, and streams an exact byte range.

- [ ] **Step 2: Verify the relevant failure**

Run: `go test ./internal/events ./internal/discovery ./internal/provenance ./internal/ui ./internal/integration`  
Expected: non-zero exit for missing behavior.

- [ ] **Step 3: Implement the minimum behavior**

Use HMAC-SHA256 over a versioned webhook envelope. Resolve/validate webhook targets against LAN rules at creation and delivery time. Implement UDP discovery with a small versioned JSON datagram rather than Plex GDM compatibility.

The provenance importer accepts explicit JSON artifacts containing source repository, source revision/digest, evidence class, target item ID, and unresolved/conflict state; it does not fetch the private repository at runtime.

The Docker image contains the server binary and expects media/config/transcode mounts. It does not enable host networking automatically; operations docs explain LAN binding choices.

- [ ] **Step 4: Verify the focused pass**

Run: `go test ./internal/events ./internal/discovery ./internal/provenance ./internal/ui ./internal/integration`  
Expected: zero exit.

- [ ] **Step 5: Run the affected integration check**

Run:
```bash
go test ./...
go vet ./...
go build ./cmd/mediaphile-server
go test -race ./...
```
Expected: all commands exit zero on a supported host; if the race detector is unavailable for a target, record that target separately rather than calling it a pass.

Inspect `git diff --check` and verify no secret/token fixtures exist.

- [ ] **Step 6: Commit the passing deliverable**

```bash
git add internal docs Dockerfile compose.yaml api/openapi.yaml README.md cmd
git commit -m "feat: complete Mediaphile server v1 slice"
```

## Cross-Repository Handoff

After Task 4 first publishes `api/openapi.yaml`, record its exact Git commit in the Mediaphile Client contract snapshot. After subsequent server tasks change the API, resynchronize the client snapshot before client integration verification.

The server does not import client source. A release/build pipeline may copy a verified `mediaphile-client/dist/` artifact into the server packaging context, but that artifact must record the client Git revision and matching OpenAPI digest.

## Unresolved Externally Observable Decisions

- **Repository licensing:** both new application repositories are public but no implementation license has been selected. This plan does not create one.
- **Brand visuals:** exact logo, palette, and typography are deliberately outside the server implementation.
- **Optional Internet metadata providers:** no provider is selected for V1. The server contract leaves room for later explicit opt-in providers without making them a runtime dependency.
