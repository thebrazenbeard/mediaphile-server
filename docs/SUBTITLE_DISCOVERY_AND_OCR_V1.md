# Subtitle discovery, OCR review, and installation V1

Mediaphile Server's first subtitle workflow is implemented in `internal/subtitles` and exposed through **admin-only** local API endpoints. The server does not modify media or embedded streams.

## V1 operations

- `GET /api/v1/libraries/{libraryId}/subtitles?offset=0&limit=25` pages cataloged media parts and checks English external sidecars and nearby local candidates.
- `GET /api/v1/subtitles/parts/{partId}` inspects a particular part, including recognized English embedded text tracks via ffmpeg extraction to memory. An unsupported image-based stream remains **unverified**, never silently declared subtitle-free.
- `POST /api/v1/subtitles/parts/{partId}/install` accepts `{"candidateId":"<opaque candidate ID>"}` for an administrator-reviewed SRT in the media directory's `Subtitles` or `Subs` inbox. The server independently repeats candidate discovery and SHA-256 validation and writes a new `.en.srt` using exclusive creation, then reads back its digest. It never overwrites an existing sidecar or video.

Candidates must be titled after the exact video filename stem, suffixed `.en.srt`, `.eng.srt`, or `.english.srt`. Unlabeled `.srt` is **not proof of English**. Foreign language suffixes do not qualify. Candidates with malformed SRT, suspicious OCR or out-of-range timing are blocked until reviewed outside this automated install seam.

The OCR audit checks a conservative vocabulary in both directions: lower-case `l` in `l'm`, `lt`, `lmpossible` and capital `I` in `Iove`, `couId`, `aII`, `Iwill`, etc. It does not auto-replace; names like `Inigo`, `Inconceivable`, and `Impossible` stay untouched. Cue order and end-of-video gaps are reviewed. SRT audit and SHA-256 verification are not full playback or speech-alignment verification.

## Separation of responsibilities and limits

The server owns filesystem access, candidate discovery, text audit, non-destructive installation, and embedded-stream extraction. The client shows administrator review results and requires an explicit install action. The private Mediaphile corpus records research provenance and subtitle evidence class, not files being rewritten.

**Not yet implemented:** downloading subtitles from Internet providers (including provider credentials/rate limits and source provenance), speech-to-text fallback, image-based subtitle OCR, guaranteed dialog-to-video synchronization, automated correction of flagged files, timed recurring search, and production/NAS deployment. The current V1 **find** implementation searches only adjacent local folders. No incomplete feature should be presented as active.

Mount NAS/media paths read-only for scanning. To install an SRT, write access to the target directory is required; failures must not trigger permission escalation or overwrite. Offline operation is the default. Maintain source media recoverability and never infer complete English subtitle coverage from unspecified-language streams.
