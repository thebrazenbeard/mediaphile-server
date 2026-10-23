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

This is a durable queue/decision foundation, not a finished automated subtitle engine. Provider-backed internet downloads, release alignment via FFsubsync/ALASS, ASR fallback, retry orchestration and autonomous publication are NOT implemented here. They require their own adapters, per-file provenance, tests and review. Historical denied NAS actions are not bypassed by this code.

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
