# Continue Watching V1

The Home screen resumes real locally stored watch history. `GET /api/v1/users/me/continue-watching?limit=12` is a bearer-authenticated and user-scoped endpoint.

It returns up to 24 unfinished movie/episode items where `resume_ms > 0` and `completed = false`, excluding unavailable media sources or parts and disabled libraries. Results are ordered by `last_played_at` newest-first with a stable item ID tie-breaker.

Each response entry contains the existing public `Item` representation, the integer `resumeMs`, and nullable `lastPlayedAt`. It never exposes another user's playback progress or raw media paths. `200 {"items":[]}` is returned when nothing can resume.

The client retains the same left navigation shell and displays a Continue Watching carousel on Home when entries exist. Clicking Resume opens the standard player, which already reads and seeks to its own saved position.

This iteration does not implement cross-device server push, collection recommendations, autoplay-next, or online watch history.
