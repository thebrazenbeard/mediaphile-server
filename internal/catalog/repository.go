package catalog

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) DB() *sql.DB { return r.db }

func (r *Repository) CreateLibrary(ctx context.Context, v Library) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO libraries(id,name,media_type,root_path,enabled)
VALUES(?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET name=excluded.name, media_type=excluded.media_type, root_path=excluded.root_path, enabled=excluded.enabled
`, v.ID, v.Name, v.MediaType, v.RootPath, boolInt(v.Enabled))
	return err
}

func (r *Repository) UpsertItem(ctx context.Context, v Item) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO items(id,library_id,parent_id,kind,title,year,season_number,episode_number,unresolved)
VALUES(?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
 library_id=excluded.library_id,parent_id=excluded.parent_id,kind=excluded.kind,title=excluded.title,
 year=excluded.year,season_number=excluded.season_number,episode_number=excluded.episode_number,
 unresolved=excluded.unresolved,updated_at=CURRENT_TIMESTAMP
`, v.ID, v.LibraryID, v.ParentID, v.Kind, v.Title, v.Year, v.SeasonNumber, v.EpisodeNumber, boolInt(v.Unresolved))
	return err
}

func (r *Repository) GetItem(ctx context.Context, id string) (Item, error) {
	var v Item
	var parent sql.NullString
	var year, season, episode sql.NullInt64
	var unresolved int
	err := r.db.QueryRowContext(ctx, `
SELECT id,library_id,parent_id,kind,title,year,season_number,episode_number,unresolved
FROM items WHERE id=?`, id).Scan(&v.ID, &v.LibraryID, &parent, &v.Kind, &v.Title, &year, &season, &episode, &unresolved)
	if err != nil {
		return Item{}, err
	}
	v.ParentID = nullStringPtr(parent)
	v.Year = nullIntPtr(year)
	v.SeasonNumber = nullIntPtr(season)
	v.EpisodeNumber = nullIntPtr(episode)
	v.Unresolved = unresolved != 0
	return v, nil
}

func (r *Repository) UpsertMediaSource(ctx context.Context, v MediaSource) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO media_sources(id,item_id,edition_id,container,duration_ms,bitrate,width,height,video_codec,audio_codec,hdr,available)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET item_id=excluded.item_id,edition_id=excluded.edition_id,container=excluded.container,
 duration_ms=excluded.duration_ms,bitrate=excluded.bitrate,width=excluded.width,height=excluded.height,
 video_codec=excluded.video_codec,audio_codec=excluded.audio_codec,hdr=excluded.hdr,available=excluded.available
`, v.ID, v.ItemID, v.EditionID, v.Container, v.DurationMS, v.Bitrate, v.Width, v.Height, v.VideoCodec, v.AudioCodec, boolInt(v.HDR), boolInt(v.Available))
	return err
}

func (r *Repository) UpsertMediaPart(ctx context.Context, v MediaPart) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO media_parts(id,source_id,path,size,mod_time_ns,available)
VALUES(?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET source_id=excluded.source_id,path=excluded.path,size=excluded.size,mod_time_ns=excluded.mod_time_ns,available=excluded.available
`, v.ID, v.SourceID, v.Path, v.Size, v.ModTimeNS, boolInt(v.Available))
	return err
}

func (r *Repository) UpsertMediaStream(ctx context.Context, v MediaStream) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO media_streams(id,part_id,kind,stream_index,codec,language,channels,width,height,frame_rate,is_default,is_forced)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET part_id=excluded.part_id,kind=excluded.kind,stream_index=excluded.stream_index,
 codec=excluded.codec,language=excluded.language,channels=excluded.channels,width=excluded.width,height=excluded.height,
 frame_rate=excluded.frame_rate,is_default=excluded.is_default,is_forced=excluded.is_forced
`, v.ID, v.PartID, v.Kind, v.StreamIndex, v.Codec, v.Language, v.Channels, v.Width, v.Height, v.FrameRate, boolInt(v.Default), boolInt(v.Forced))
	return err
}

func (r *Repository) MediaInventory(ctx context.Context, itemID string) ([]MediaSource, []MediaPart, []MediaStream, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id,item_id,edition_id,container,duration_ms,bitrate,width,height,video_codec,audio_codec,hdr,available
FROM media_sources WHERE item_id=? ORDER BY id`, itemID)
	if err != nil {
		return nil, nil, nil, err
	}
	var sources []MediaSource
	for rows.Next() {
		var v MediaSource
		var edition sql.NullString
		var hdr, available int
		if err := rows.Scan(&v.ID, &v.ItemID, &edition, &v.Container, &v.DurationMS, &v.Bitrate, &v.Width, &v.Height, &v.VideoCodec, &v.AudioCodec, &hdr, &available); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		v.EditionID = nullStringPtr(edition)
		v.HDR = hdr != 0
		v.Available = available != 0
		sources = append(sources, v)
	}
	if err := rows.Close(); err != nil {
		return nil, nil, nil, err
	}

	rows, err = r.db.QueryContext(ctx, `
SELECT p.id,p.source_id,p.path,p.size,p.mod_time_ns,p.available
FROM media_parts p JOIN media_sources s ON s.id=p.source_id
WHERE s.item_id=? ORDER BY p.id`, itemID)
	if err != nil {
		return nil, nil, nil, err
	}
	var parts []MediaPart
	for rows.Next() {
		var v MediaPart
		var available int
		if err := rows.Scan(&v.ID, &v.SourceID, &v.Path, &v.Size, &v.ModTimeNS, &available); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		v.Available = available != 0
		parts = append(parts, v)
	}
	if err := rows.Close(); err != nil {
		return nil, nil, nil, err
	}

	rows, err = r.db.QueryContext(ctx, `
SELECT st.id,st.part_id,st.kind,st.stream_index,st.codec,st.language,st.channels,st.width,st.height,st.frame_rate,st.is_default,st.is_forced
FROM media_streams st JOIN media_parts p ON p.id=st.part_id JOIN media_sources s ON s.id=p.source_id
WHERE s.item_id=? ORDER BY st.part_id,st.stream_index`, itemID)
	if err != nil {
		return nil, nil, nil, err
	}
	var streams []MediaStream
	for rows.Next() {
		var v MediaStream
		var def, forced int
		if err := rows.Scan(&v.ID, &v.PartID, &v.Kind, &v.StreamIndex, &v.Codec, &v.Language, &v.Channels, &v.Width, &v.Height, &v.FrameRate, &def, &forced); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		v.Default = def != 0
		v.Forced = forced != 0
		streams = append(streams, v)
	}
	if err := rows.Close(); err != nil {
		return nil, nil, nil, err
	}
	return sources, parts, streams, nil
}

func (r *Repository) SetMediaPartAvailable(ctx context.Context, id string, available bool) error {
	res, err := r.db.ExecContext(ctx, "UPDATE media_parts SET available=? WHERE id=?", boolInt(available), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *Repository) CreatePrincipal(ctx context.Context, v Principal) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO principals(id,username,password_hash,is_admin) VALUES(?,?,?,?)
ON CONFLICT(id) DO UPDATE SET username=excluded.username,password_hash=excluded.password_hash,is_admin=excluded.is_admin
`, v.ID, v.Username, v.PasswordHash, boolInt(v.Admin))
	return err
}

func (r *Repository) PutPlaybackState(ctx context.Context, v PlaybackState) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO playback_state(principal_id,item_id,resume_ms,play_count,completed,last_played_at,selected_audio_stream_id,selected_subtitle_stream_id)
VALUES(?,?,?,?,?,?,?,?)
ON CONFLICT(principal_id,item_id) DO UPDATE SET
 resume_ms=excluded.resume_ms,play_count=excluded.play_count,completed=excluded.completed,last_played_at=excluded.last_played_at,
 selected_audio_stream_id=excluded.selected_audio_stream_id,selected_subtitle_stream_id=excluded.selected_subtitle_stream_id
`, v.PrincipalID, v.ItemID, v.ResumeMS, v.PlayCount, boolInt(v.Completed), v.LastPlayedAt, v.SelectedAudioStreamID, v.SelectedSubtitleStreamID)
	return err
}

func (r *Repository) GetPlaybackState(ctx context.Context, principalID, itemID string) (PlaybackState, error) {
	var v PlaybackState
	var completed int
	var last, audio, subtitle sql.NullString
	err := r.db.QueryRowContext(ctx, `
SELECT principal_id,item_id,resume_ms,play_count,completed,last_played_at,selected_audio_stream_id,selected_subtitle_stream_id
FROM playback_state WHERE principal_id=? AND item_id=?`, principalID, itemID).
		Scan(&v.PrincipalID, &v.ItemID, &v.ResumeMS, &v.PlayCount, &completed, &last, &audio, &subtitle)
	if err != nil {
		return PlaybackState{}, err
	}
	v.Completed = completed != 0
	v.LastPlayedAt = nullStringPtr(last)
	v.SelectedAudioStreamID = nullStringPtr(audio)
	v.SelectedSubtitleStreamID = nullStringPtr(subtitle)
	return v, nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func nullStringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	x := v.String
	return &x
}

func nullIntPtr(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	x := int(v.Int64)
	return &x
}

func IsNotFound(err error) bool { return errors.Is(err, sql.ErrNoRows) }

func (r *Repository) GetLibrary(ctx context.Context, id string) (Library, error) {
	var v Library
	var enabled int
	err := r.db.QueryRowContext(ctx, "SELECT id,name,media_type,root_path,enabled FROM libraries WHERE id=?", id).
		Scan(&v.ID, &v.Name, &v.MediaType, &v.RootPath, &enabled)
	if err != nil {
		return Library{}, err
	}
	v.Enabled = enabled != 0
	return v, nil
}

func (r *Repository) FindMediaPartByPath(ctx context.Context, path string) (MediaPart, error) {
	var v MediaPart
	var available int
	err := r.db.QueryRowContext(ctx, "SELECT id,source_id,path,size,mod_time_ns,available FROM media_parts WHERE path=?", path).
		Scan(&v.ID, &v.SourceID, &v.Path, &v.Size, &v.ModTimeNS, &available)
	if err != nil {
		return MediaPart{}, err
	}
	v.Available = available != 0
	return v, nil
}

func (r *Repository) ListPartsByLibrary(ctx context.Context, libraryID string) ([]MediaPart, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT p.id,p.source_id,p.path,p.size,p.mod_time_ns,p.available
FROM media_parts p
JOIN media_sources s ON s.id=p.source_id
JOIN items i ON i.id=s.item_id
WHERE i.library_id=?
ORDER BY p.path`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MediaPart
	for rows.Next() {
		var v MediaPart
		var available int
		if err := rows.Scan(&v.ID, &v.SourceID, &v.Path, &v.Size, &v.ModTimeNS, &available); err != nil {
			return nil, err
		}
		v.Available = available != 0
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *Repository) DeleteStreamsForPart(ctx context.Context, partID string) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM media_streams WHERE part_id=?", partID)
	return err
}

type ItemQuery struct {
	LibraryID  string
	Kind       ItemKind
	ParentID   string
	Search     string
	AfterTitle string
	AfterID    string
	Limit      int
}

func (r *Repository) ListLibraries(ctx context.Context) ([]Library, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT id,name,media_type,root_path,enabled FROM libraries ORDER BY name,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Library
	for rows.Next() {
		var v Library
		var enabled int
		if err := rows.Scan(&v.ID, &v.Name, &v.MediaType, &v.RootPath, &enabled); err != nil {
			return nil, err
		}
		v.Enabled = enabled != 0
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *Repository) ListItems(ctx context.Context, q ItemQuery) ([]Item, error) {
	query := "SELECT id,library_id,parent_id,kind,title,year,season_number,episode_number,unresolved FROM items WHERE 1=1"
	args := []any{}
	if q.LibraryID != "" {
		query += " AND library_id=?"
		args = append(args, q.LibraryID)
	}
	if q.Kind != "" {
		query += " AND kind=?"
		args = append(args, q.Kind)
	}
	if q.ParentID != "" {
		query += " AND parent_id=?"
		args = append(args, q.ParentID)
	}
	if q.Search != "" {
		query += " AND lower(title) LIKE ?"
		args = append(args, "%"+strings.ToLower(q.Search)+"%")
	}
	if q.AfterTitle != "" || q.AfterID != "" {
		query += " AND (lower(title)>? OR (lower(title)=? AND id>?))"
		args = append(args, strings.ToLower(q.AfterTitle), strings.ToLower(q.AfterTitle), q.AfterID)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 201 {
		limit = 201
	}
	query += " ORDER BY lower(title),id LIMIT ?"
	args = append(args, limit)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var v Item
		var parent sql.NullString
		var year, season, episode sql.NullInt64
		var unresolved int
		if err := rows.Scan(&v.ID, &v.LibraryID, &parent, &v.Kind, &v.Title, &year, &season, &episode, &unresolved); err != nil {
			return nil, err
		}
		v.ParentID = nullStringPtr(parent)
		v.Year = nullIntPtr(year)
		v.SeasonNumber = nullIntPtr(season)
		v.EpisodeNumber = nullIntPtr(episode)
		v.Unresolved = unresolved != 0
		out = append(out, v)
	}
	return out, rows.Err()
}
