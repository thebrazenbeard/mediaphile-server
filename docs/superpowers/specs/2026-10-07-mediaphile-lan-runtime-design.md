# Mediaphile LAN Server Design

**Date:** 2026-10-07  
**Repository:** `thebrazenbeard/mediaphile-server`  
**Status:** Design approved in chat; written specification awaiting explicit review before implementation.

## 1. Purpose

Mediaphile Server is the LAN-native runtime for the Mediaphile ecosystem. It turns local media storage into a browsable, playable library without requiring a Plex account, cloud relay, remote-access service, or Internet dependency.

The server owns media discovery, catalog persistence, local identity and authorization, client capability negotiation, playback decisions, direct streaming, remux/transcode orchestration, playback-state persistence, and local event/webhook delivery.

The existing private `thebrazenbeard/mediaphile` repository remains the provenance and knowledge authority. This repository is an executable runtime, not a replacement for that corpus.

## 2. Product Invariants

1. **LAN ingress only.** By default the HTTP API accepts loopback and private/link-local LAN addresses only. Public/WAN source addresses are rejected before authentication. Administrators may narrow the allowed CIDRs but v1 provides no setting that expands access to arbitrary public networks.
2. **No remote-access machinery.** No UPnP, NAT-PMP, PCP, automatic router configuration, relay service, cloud tunnel, or public discovery endpoint is implemented.
3. **No cloud identity requirement.** Local playback and administration must continue when the Internet is unavailable.
4. **Internet-independent media access.** Existing catalog and playback behavior cannot depend on an external metadata service being reachable.
5. **Optional outbound metadata only.** Metadata providers may be added later behind explicit configuration. They are disabled by default and are not an authentication or availability dependency.
6. **LAN does not equal administrator.** Network location is an access boundary, not an identity boundary. Administrative effects require a local authenticated principal.
7. **Direct Play first.** The server chooses the least-transformative valid playback path: Direct Play, then Remux, then Transcode.
8. **Files are not logical works.** A movie/episode identity is separate from its editions, encodes, parts, and elementary streams.
9. **Server API is the client contract.** Clients do not access the SQLite database, media filesystem paths, or private Mediaphile corpus directly.
10. **Source-first reconstruction.** Runtime state that matters must be reproducible from this repository plus configured media roots and explicit local state.

## 3. V1 Scope

The first useful vertical slice supports:

- local Movies and TV libraries;
- configurable local/SMB-mounted filesystem roots;
- deterministic scanning and incremental rescan;
- media probing through `ffprobe`;
- SQLite catalog storage on server-local reliable storage;
- title/year and show/season/episode filename parsing with unresolved identities preserved rather than guessed;
- library browsing and text search;
- item detail with media/stream inventory;
- local users and session tokens;
- client capability reporting;
- playback decision endpoint;
- HTTP byte-range Direct Play;
- container Remux and HLS Transcode through `ffmpeg`;
- resume position and watched state;
- playback-session lifecycle;
- local event bus and outbound LAN webhook subscriptions;
- static serving of a built Mediaphile Client bundle;
- health/readiness endpoints;
- structured logs;
- Linux, Windows, and Linux ARM builds where the selected Go/SQLite dependencies support them.

Music, photos, DVR/live TV, WAN access, federation, cloud sync, downloads/sync-to-device, commercial metadata agents, and Plex protocol compatibility are outside v1.

## 4. Technology

The implementation will use:

- **Go** for the server process and standard-library HTTP stack;
- **SQLite** for durable local catalog/state;
- a pure-Go SQLite driver so normal cross-compilation does not require CGO;
- **FFmpeg / ffprobe** as explicit external media tools;
- JSON over HTTP for the application API;
- HLS for transcoded browser playback;
- UDP local discovery for native/TV-capable clients;
- OpenAPI 3 as the machine-readable public contract.

The server must start without FFmpeg only if no operation requiring probing/transcoding is requested. Readiness reports FFmpeg/ffprobe capability separately so missing media tools are visible rather than silently ignored.

## 5. Repository Structure

Proposed initial structure:

```text
cmd/mediaphile-server/       process entry point
internal/config/              config parsing and validation
internal/netguard/            LAN source-address enforcement
internal/auth/                local principals and session tokens
internal/catalog/             catalog persistence and queries
internal/library/             library definitions and scan orchestration
internal/probe/               ffprobe boundary and normalized stream facts
internal/identity/            filename/path identity extraction
internal/playback/            capability model and decision engine
internal/stream/              byte-range direct streaming
internal/transcode/           ffmpeg process/session management
internal/events/              in-process events and webhook delivery
internal/httpapi/             versioned HTTP handlers
internal/discovery/           LAN discovery responder
internal/ui/                  static client bundle serving
migrations/                   ordered SQLite migrations
api/openapi.yaml              authoritative external API contract
testdata/                     deterministic media/probe fixtures
docs/                         operations and architecture
```

Packages expose narrow interfaces so probing, persistence, and process execution can be replaced by deterministic test fakes without duplicating production algorithms.

## 6. Catalog Model

The initial schema separates semantic identity from storage representation.

### Library

A configured collection with an ID, name, media type, root path, enabled state, scan timestamps, and scan policy.

### Item

A logical playable or grouping entity. V1 item kinds are `movie`, `show`, `season`, and `episode`.

A show owns seasons; a season owns episodes. A movie and episode are playable logical works.

### Edition

An optional named presentation of a work, such as theatrical, director's cut, restored, or extended. An unresolved source may omit edition instead of inventing one.

### MediaSource

One encoded representation of a playable item or edition: container, duration, bitrate, resolution, codecs, HDR indicators, and availability.

### MediaPart

One physical file participating in a MediaSource. Multipart media is represented explicitly rather than flattened into a guessed single file.

### MediaStream

An elementary video, audio, or subtitle stream with codec, language, channel layout, dimensions, frame rate, disposition/default/forced flags, and stream index.

### Principal / Session

A local user and authenticated client session. Network location never substitutes for these records.

### PlaybackState

Per-principal item state: resume offset, play count, completed flag, last played time, selected audio/subtitle preferences where known.

### PlaybackSession

One active or completed playback execution: item, principal, client, selected source/streams, decision, timestamps, progress, and stop reason.

### WebhookSubscription

A local or explicitly configured target URL plus subscribed event types and signing secret reference.

## 7. Library Scanning

A scan runs as a bounded job:

1. enumerate supported media files beneath an enabled library root;
2. normalize paths without following links outside the configured root;
3. compare path, size, and modification data with existing file observations;
4. probe new or materially changed files through the `Probe` interface;
5. parse filename/path identity candidates;
6. reconcile only deterministic matches;
7. retain ambiguous/unmatched items as unresolved catalog records;
8. mark missing files unavailable without immediately deleting historical identity/state;
9. commit scan results transactionally in bounded batches;
10. emit scan completion/failure events with counts.

A scanner must not rename, move, delete, or rewrite media. Existing rename/normalization authority in the private Mediaphile corpus remains separate.

## 8. Provenance Integration

The server may import curated identity and knowledge material from `thebrazenbeard/mediaphile` through explicit generated/import artifacts. The private repository is never a runtime dependency that clients must fetch.

Imported knowledge records carry:

- source repository;
- source revision or artifact digest;
- source path/record identifier where available;
- evidence/provenance class;
- import timestamp;
- normalized target item ID;
- unresolved/conflict state.

Filesystem presence, external metadata, transcript/screenplay evidence, subtitle evidence, audiovisual review, and derived interpretation remain distinguishable. Import never upgrades one evidence class into another.

## 9. Local Network Guard

The HTTP listener may bind to `0.0.0.0` or a configured interface so LAN clients can reach it, but every request passes through a source-address guard.

Default accepted source ranges:

- loopback;
- RFC1918 IPv4 private ranges;
- IPv4 link-local;
- IPv6 loopback, link-local, and unique-local addresses.

Forwarded headers are ignored unless an explicitly configured trusted reverse-proxy address supplied the request. This prevents a remote caller from forging a private `X-Forwarded-For` value.

The server advertises no public URL.

Default HTTP port: `8097`.  
Default UDP discovery port: `8098`.

Both are configurable.

## 10. Authentication

V1 uses local accounts only.

Initial setup creates the first administrator through a one-time bootstrap secret emitted to the local console and stored only as a verifier after successful use. Subsequent authentication returns an opaque random session token.

Rules:

- bootstrap is accepted only while no administrator exists;
- passwords are stored using a memory-hard password hash;
- session tokens are stored hashed;
- administrative endpoints require an administrator principal;
- ordinary playback/browse requests require an authenticated local principal;
- logout/revocation takes effect immediately;
- no IP-address-based admin bypass exists.

## 11. Client Capability Model

A playback request includes a normalized capability profile:

```json
{
  "clientId": "stable-local-client-id",
  "containers": ["mp4", "webm"],
  "videoCodecs": ["h264", "vp9"],
  "audioCodecs": ["aac", "opus"],
  "subtitleCodecs": ["webvtt"],
  "maxWidth": 3840,
  "maxHeight": 2160,
  "maxVideoBitrate": 40000000,
  "hls": true,
  "rangeRequests": true
}
```

Capabilities describe demonstrated client support. They are not inferred from a user-agent string when the client can report them directly.

## 12. Playback Decision Engine

Input:

- logical item and candidate media sources;
- selected audio/subtitle streams;
- client capability profile;
- user playback preferences;
- server FFmpeg capability;
- optional quality ceiling.

Output:

```text
DIRECT_PLAY
REMUX
TRANSCODE
UNPLAYABLE
```

The decision is deterministic for the same normalized input.

### Direct Play

Selected source container and required elementary streams are supported by the client, bitrate/resolution limits are satisfied, selected subtitles can remain soft, and the file is locally accessible.

### Remux

Video/audio codecs are client-compatible but the source container is not, or stream selection requires repackaging without codec conversion.

### Transcode

At least one required audio/video codec or bound is incompatible, or subtitle burn-in is required and FFmpeg can produce an allowed HLS target.

### Unplayable

No accessible media source can satisfy the request and the server cannot perform the required transform.

The response includes the chosen source, chosen streams, reason codes, and a server URL. Clients never receive an absolute filesystem path.

## 13. HTTP API V1

The public contract lives at `api/openapi.yaml`.

Core endpoints:

```text
GET    /api/v1/health
GET    /api/v1/server
POST   /api/v1/setup/bootstrap
POST   /api/v1/auth/login
POST   /api/v1/auth/logout

GET    /api/v1/libraries
POST   /api/v1/libraries
POST   /api/v1/libraries/{libraryId}/scan

GET    /api/v1/items
GET    /api/v1/items/{itemId}

POST   /api/v1/playback/decide
POST   /api/v1/playback/sessions
PATCH  /api/v1/playback/sessions/{sessionId}
DELETE /api/v1/playback/sessions/{sessionId}

GET    /api/v1/media/{partId}/content
GET    /api/v1/transcode/{sessionId}/master.m3u8
GET    /api/v1/transcode/{sessionId}/{segment}

GET    /api/v1/users/me/playback/{itemId}
PUT    /api/v1/users/me/playback/{itemId}

GET    /api/v1/events
GET    /api/v1/webhooks
POST   /api/v1/webhooks
DELETE /api/v1/webhooks/{webhookId}
```

Pagination uses opaque cursors. IDs are opaque strings. JSON errors use a stable machine code plus human message and optional field detail.

## 14. Direct Streaming

Direct Play uses ordinary HTTP range semantics:

- `Accept-Ranges: bytes`;
- valid single byte ranges return `206 Partial Content`;
- invalid/unsatisfiable ranges return `416`;
- HEAD returns metadata without body;
- content type is derived from the probed source/container and never from unsanitized user input;
- the requested part ID is resolved through the catalog, not accepted as a raw path parameter.

## 15. Transcode Lifecycle

Transcoding is session-scoped.

The server:

1. creates a playback session;
2. derives an FFmpeg command from a typed decision object;
3. writes output under a dedicated local transcode directory;
4. exposes only that session's generated playlist/segments;
5. records process PID and start time;
6. terminates FFmpeg when the session ends or expires;
7. deletes expired transcode artifacts without touching source media.

Command construction is argument-vector based, never shell-string concatenation.

## 16. Events and Webhooks

Internal event types initially include:

- `library.scan.started`;
- `library.scan.completed`;
- `library.scan.failed`;
- `media.play`;
- `media.resume`;
- `media.pause`;
- `media.progress`;
- `media.stop`;
- `media.completed`.

Webhooks receive a versioned JSON envelope containing event ID, event type, timestamp, principal/client identifiers where permitted, and media identity. Deliveries are signed. Failures use bounded retries and never block playback.

Default webhook targets must resolve to allowed LAN addresses. Public targets require a future explicit product decision and are not part of v1.

## 17. Client Hosting and Discovery

For browsers, the server can serve a built `mediaphile-client` bundle at `/`. Same-origin use is the default zero-configuration browser path.

Browser JavaScript cannot perform raw UDP discovery, so browser discovery is not faked. A standalone browser build accepts a manually supplied server URL.

Native/TV wrappers may use Mediaphile Discovery V1 over UDP `8098`. The protocol is intentionally Mediaphile-specific rather than Plex GDM compatibility.

A discovery query identifies protocol version and client ID. A response contains server ID, human name, HTTP port, API version, and capability flags. Discovery responses do not contain credentials.

## 18. Configuration and Storage

Configuration precedence:

1. command-line flags;
2. environment variables;
3. config file;
4. documented defaults.

Separate directories are used for:

- durable config/database;
- metadata/artwork cache;
- temporary transcode output;
- logs when file logging is enabled.

SQLite must live on server-local storage with reliable file locking. Media roots may be network mounts. The server warns and refuses an explicitly detected network-share database path when detection is reliable.

## 19. Observability

Structured logs include request ID, session ID, scan ID, decision, media item ID, and duration where relevant. Secrets, passwords, session tokens, raw authorization headers, and webhook signing secrets are never logged.

Health is split:

- liveness: process/event loop alive;
- readiness: database available and migrations complete;
- capabilities: FFmpeg/ffprobe presence and version, writable transcode directory, configured media-root reachability.

## 20. Testing Strategy

The server will be developed through public seams.

Required tests include:

- LAN guard accepts private/loopback and rejects public sources;
- spoofed forwarded headers do not bypass the guard;
- bootstrap works once and cannot be replayed;
- unauthenticated administration is rejected;
- scanner is idempotent for unchanged files;
- changed files are reprobed;
- missing files become unavailable without destroying playback state;
- ambiguous identity remains unresolved;
- catalog hierarchy queries preserve movie/show/season/episode relationships;
- Direct Play decision for supported container/codecs;
- Remux decision for compatible codecs/incompatible container;
- Transcode decision for codec/subtitle incompatibility;
- Unplayable when transform capability is unavailable;
- byte-range streaming returns correct status/body boundaries;
- transcode command construction cannot interpret media paths as shell syntax;
- session progress persists per principal;
- webhook failure cannot fail playback;
- migrations apply from empty database and are repeatable;
- OpenAPI contract validates;
- Windows and Linux builds pass;
- an integration test exercises scan -> browse -> decide -> range-stream using a temporary catalog and deterministic probe fake.

Tests requiring the real FFmpeg binary are separated from unit/integration tests and report environment capability explicitly.

## 21. Compatibility and Versioning

The API begins at `/api/v1`.

Breaking request/response changes require a new API version. Additive fields may be introduced within v1 when old clients can ignore them safely.

The client repository consumes a generated type snapshot from this OpenAPI document. The client records the source contract digest so contract updates are deliberate and reviewable.

## 22. Initial Implementation Order

After this written specification is approved:

1. repository/toolchain baseline plus health endpoint and LAN guard;
2. SQLite migrations and catalog repository;
3. scanner/probe/identity slice;
4. browse/detail API and OpenAPI contract;
5. local auth/bootstrap;
6. playback capability model and decision engine;
7. byte-range Direct Play;
8. FFmpeg remux/transcode session manager;
9. playback-state/session persistence and events;
10. webhooks and discovery;
11. static client bundle hosting and packaging;
12. cross-repo end-to-end verification.

Each step must have a focused failing test before production behavior is added where practical.

## 23. Deliberate Non-Compatibility

Mediaphile does not emulate Plex endpoints, Plex GDM packet identity, Plex tokens, or Plex client profiles. Plex repositories are behavioral research references only unless a specifically permissive license and explicit reuse decision apply.

This avoids locking Mediaphile to undocumented compatibility behavior and avoids accidental GPL-derived implementation in the proprietary/source-visible Mediaphile corpus.

## 24. V1 Success Criteria

V1 is successful when, on a machine with a mounted local media library:

1. the server starts with no Internet connection;
2. a local administrator can bootstrap and log in;
3. a movie or episode library can be scanned into SQLite;
4. the web client can list and open catalog items;
5. the client can report capabilities and receive a deterministic playback decision;
6. a browser can Direct Play a compatible file with seeking;
7. an incompatible file can be remuxed or transcoded when FFmpeg is available;
8. progress survives server/client restart;
9. a second LAN machine can use the client;
10. a public-source-address request is rejected;
11. no router configuration or Plex/cloud service is required;
12. the existing Mediaphile knowledge layer can be imported without collapsing provenance classes.

## 25. Assumptions Chosen for V1

The phrase "local network only" is interpreted as **LAN-only service exposure and no cloud dependency**, not a permanent ban on every outbound Internet request. Optional metadata providers may later make outbound requests when explicitly enabled, but the server is fully usable without them.

No license is selected by this specification. The repositories currently have no implementation license; choosing one is a separate owner decision.
