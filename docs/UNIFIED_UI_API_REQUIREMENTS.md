# Server-side requirements for the unified Mediaphile UI

**Authority:** Owner-approved single-shell client UX decision (2026-10-07).

Mediaphile Server provides one versioned API for all clients. It does not issue special catalogs, different route hierarchies or different media identity formats for phones versus TVs.

The `GET /api/v1/items` endpoint supports:

- `libraryId` to select a configured library; empty means all accessible libraries;
- `kind` to select logical `movie`, `show`, `season`, or `episode` items;
- `q` to search local titles;
- `watchState` = `all`, `unplayed`, `in_progress`, `watched`, with the latter three computed from the **authenticated principal's own** playback state;
- `cursor` as an opaque, stable page continuation token;
- `limit` from 1 through 200.

The client should not load an entire library merely to perform sorting, status filtering or search. Current server ordering is title ascending; additional sort orders require an explicit API/cursor contract change before they appear in a UI.

Playback source URLs are server-issued, part IDs are opaque, and actual filesystem paths never leave the API. Read-only byte-range and HLS media requests accept the existing HttpOnly session cookie, allowing native browser media elements to stream from the same origin without bearer tokens in URLs.

This requirement neither grants WAN access nor cloud dependencies. UI research does not authorize merging the source repo's ingestion PRs or modifying media files.
