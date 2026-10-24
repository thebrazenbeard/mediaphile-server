# Subtitle Manager state V1

Source-only feature based on Mediaphile Server subtitle discovery draft PR #7. Not installed on DSM.

The executable subtitle manager belongs in mediaphile-server (the LAN Go server), while mediaphile-client shows the controls in the existing unified Settings interface. The private mediaphile repository is a knowledge corpus, not the runtime.

## Decisions and coverage

A local SQLite migration stores a per-media-part disposition: active (default), skip (owner does not want subtitles), or review (manual hold). Reasons are required for skip and review. The server reports the decision independently of true subtitle coverage: skips DO NOT turn missing subtitles into installed subtitles. Advisory next actions are excluded, manual_review, covered, inspect_existing, review_local_candidate or search_published.

## Admin API

GET /api/v1/subtitles/parts/{partId}/decision retrieves the decision.
PUT /api/v1/subtitles/parts/{partId}/decision saves it with a JSON body containing disposition and reason.
POST /api/v1/subtitles/decisions/import accepts a JSON object with an exclusions array of objects with video (exact catalog path) and reason. Entries are checked against available media parts and imported atomically. An unknown video prevents ALL entries being imported, and existing decisions always take precedence.

The Settings page supports explicit user decisions and manual JSON selection/import after the live server catalog has indexed the media. There is no silent read of the historical Lappy checkpoint.

These endpoints never open, mutate, remove, or rename a media file or subtitle. Existing subtitle installation remains a separately authorized exclusive-create operation.

## Limits of this phase

This is a durable queue/decision foundation, not a finished automated subtitle engine. Later sections document bounded provider-backed **review staging** and internal FFsubsync staging; automatic release alignment, ASR fallback, retry orchestration and autonomous publication are NOT implemented here. Historical denied NAS actions are not bypassed by this code.

Source tests and UI builds are not evidence of DSM install or playback qualification. Merge and production deployment require separate authorization and verification.


## Incremental queue planning (V1.1)

Migration 004_subtitle_queue.sql persists one advisory observation per media
part. Admin-only POST /api/v1/libraries/{libraryId}/subtitles/queue/refresh
inspects a bounded page of 1-50 indexed media parts, including sidecars and
current active/skip/review decisions, then commits all queue observations in
one SQLite transaction. Any filesystem inspection failure holds the entire
page without changing existing queue records. The response lists only that
media page; GET /api/v1/libraries/{libraryId}/subtitles/queue reads the
persisted queued observations.

Actions: excluded, manual_review, covered, inspect_existing,
review_local_candidate, search_published. Existing English sidecars with
quality warnings get inspect_existing rather than covered. Manual
candidate installation is disallowed until a skip/review decision is returned
to active. No Internet access, speech transcription, subtitle replacement,
or automatic publication occurs during refresh.

Queue observations are cached and may be stale after media changes until
refreshed. Pagination applies to currently indexed media on POST and to stored
queue records on GET. Stable media-part IDs survive rescans of the same path.
The UI exposes an explicit Inspect current page into recovery queue button,
and no invisible background worker is started.

## Staged FFsubsync adapter (V1.2, internal library only)

The server includes a tested subtitles.StageSync adapter. It accepts a video,
existing SRT source, trusted explicit ffsubsync executable path and a separate
staging root outside the media directory. It directly invokes ffsubsync with
explicit input and output arguments, low-quality rejection, 8 audio sample
segments, and strict subtitle parsing. Execution is limited to 15 minutes.

It validates non-symlink regular files, checks that the original source and
media have not changed during processing, hashes both subtitle versions,
audits the staged result and cleans failed staging attempts. It never publishes
a sidecar, overwrites the source SRT, or modifies the video. No shell command
interpolation or Internet subtitle provider is involved.

ReadyForReview means the staged SRT is structurally accepted and differs from
the source. It does not certify correct release identity, audiovisual sync,
complete spoken dialogue, or authorization to publish. Unchanged results are
held for inspection.

This adapter is internal-only, with test-double validation; no production
worker, endpoint, scheduled run, installed ffsubsync binary, or NAS runtime
qualification exists. Integration must supply a trusted executable and a
separate staging volume, durable provenance and a reviewed publication gate.
Previously denied NAS recovery operations remain held.


## Explicit read-only published subtitle discovery (V1.3)

The admin-only POST /api/v1/subtitles/parts/{partId}/published/search
endpoint supports manual provider metadata lookups. It takes a title query,
optional IMDb identifier, or a default title derived from the selected
media basename. It never transmits the full NAS media path, uploads video,
downloads subtitle files, modifies sidecars, or schedules repeat requests.
Skip/review dispositions hold this endpoint before any external request.

When configured, the server reads MEDIAPHILE_OPENSUBTITLES_API_KEY from its
runtime environment and sends a single authenticated request to the fixed
https://api.opensubtitles.com/api/v1/subtitles endpoint. Missing keys return
503 without calling the provider. Credentials must not be checked into
source control or included in logs. Search requires explicit admin UI action;
this environment variable is NOT installed in any NAS runtime by this PR.

The adapter handles redirects without following them, provider rate limits
without hammering/retrying, size bounds on JSON, and filters non-English,
machine-translated, AI-translated, and foreign-parts-only results. Candidate
metadata (source ID, file ID, release, trusted flag) is advisory: exact cuts,
timing, real transcription fidelity, license, and source provenance must still
be verified before a future separately authorized download/install workflow.
The OpenSubtitles API is an external provider with its own keys and quotas.

## Explicit isolated published-SRT staging (V1.4, source only)

POST /api/v1/subtitles/parts/{partId}/published/stage accepts a selected
`{"fileId": 123}` from the manual search result. It is admin-only, denies
skip/review dispositions, and requires three operator-configured environment
values: `MEDIAPHILE_OPENSUBTITLES_API_KEY`,
`MEDIAPHILE_OPENSUBTITLES_TOKEN` (the account bearer token), and
`MEDIAPHILE_SUBTITLE_STAGE_ROOT` (an existing private directory separate
from the media directory). Missing configuration returns 503. No credential
or provider temporary URL is persisted by the adapter.

The provider download-ticket POST goes only to the fixed OpenSubtitles API
host. A temporary HTTPS file URL is accepted only from the specific
`www.opensubtitles.com/download/` path; the follow-up GET carries neither
API key nor bearer token. Redirects are rejected. The result is limited to
2 MiB, validated as UTF-8 SRT, audited against indexed media duration, and
written with an exclusive create under a new private staging directory.
Invalid or failed attempts remove their staging artifacts. It checks that
the indexed video did not change during the operation and never writes a
media sidecar. The endpoint returns a staged SHA-256 and audit as an
**unverified review candidate**, not installation approval. Each explicit
request may spend provider quota; no automatic retry is performed.

This is a server/API slice only. It has mocked-network tests but no live
provider credential, NAS installation, provider playback comparison, worker,
or automatic publishing. A future client control must separately expose
the explicit stage action and review state; no silent background downloads.
