package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
	"github.com/thebrazenbeard/mediaphile-server/internal/playback"
)

func createPlaybackSession(deps Dependencies) http.HandlerFunc {
	type request struct {
		ItemID   string        `json:"itemId"`
		ClientID string        `json:"clientId"`
		SourceID string        `json:"sourceId"`
		Decision playback.Mode `json:"decision"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Sessions == nil {
			writeError(w, http.StatusServiceUnavailable, "PLAYBACK_UNAVAILABLE", "playback session service is unavailable")
			return
		}
		principal, ok := principalFromRequest(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
			return
		}
		var in request
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.ItemID == "" || in.ClientID == "" || in.SourceID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "itemId, clientId and sourceId are required")
			return
		}
		v, err := deps.Sessions.Create(r.Context(), principal.ID, in.ItemID, in.ClientID, in.SourceID, in.Decision)
		if err != nil {
			writeError(w, http.StatusBadRequest, "PLAYBACK_SESSION_ERROR", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, sessionDTO(v))
	}
}

func updatePlaybackSession(deps Dependencies) http.HandlerFunc {
	type request struct {
		PositionMS int64  `json:"positionMs"`
		State      string `json:"state"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Sessions == nil {
			writeError(w, http.StatusServiceUnavailable, "PLAYBACK_UNAVAILABLE", "playback session service is unavailable")
			return
		}
		principal, ok := principalFromRequest(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
			return
		}
		var in request
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body is invalid")
			return
		}
		v, err := deps.Sessions.Update(r.Context(), principal.ID, r.PathValue("sessionId"), in.PositionMS, in.State)
		if errors.Is(err, playback.ErrSessionForbidden) {
			writeError(w, http.StatusForbidden, "SESSION_FORBIDDEN", "playback session belongs to another user")
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "playback session not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "PLAYBACK_SESSION_ERROR", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, sessionDTO(v))
	}
}

func deletePlaybackSession(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Sessions == nil {
			writeError(w, http.StatusServiceUnavailable, "PLAYBACK_UNAVAILABLE", "playback session service is unavailable")
			return
		}
		principal, ok := principalFromRequest(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
			return
		}
		reason := r.URL.Query().Get("reason")
		if err := deps.Sessions.End(r.Context(), principal.ID, r.PathValue("sessionId"), reason); err != nil {
			if errors.Is(err, playback.ErrSessionForbidden) {
				writeError(w, http.StatusForbidden, "SESSION_FORBIDDEN", "playback session belongs to another user")
			} else {
				writeError(w, http.StatusNotFound, "NOT_FOUND", "playback session not found")
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func getPlaybackState(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := principalFromRequest(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
			return
		}
		state, err := deps.Catalog.GetPlaybackState(r.Context(), principal.ID, r.PathValue("itemId"))
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "playback state not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CATALOG_ERROR", "could not load playback state")
			return
		}
		writeJSON(w, http.StatusOK, playbackStateDTO(state))
	}
}

func putPlaybackState(deps Dependencies) http.HandlerFunc {
	type request struct {
		ResumeMS  int64 `json:"resumeMs"`
		Completed bool  `json:"completed"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := principalFromRequest(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
			return
		}
		var in request
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body is invalid")
			return
		}
		state := catalog.PlaybackState{PrincipalID: principal.ID, ItemID: r.PathValue("itemId"), ResumeMS: in.ResumeMS, Completed: in.Completed}
		if old, err := deps.Catalog.GetPlaybackState(r.Context(), principal.ID, state.ItemID); err == nil {
			state.PlayCount = old.PlayCount
		}
		if err := deps.Catalog.PutPlaybackState(r.Context(), state); err != nil {
			writeError(w, http.StatusBadRequest, "PLAYBACK_STATE_ERROR", "could not update playback state")
			return
		}
		writeJSON(w, http.StatusOK, playbackStateDTO(state))
	}
}

func sessionDTO(v catalog.PlaybackSession) map[string]any {
	return map[string]any{"id": v.ID, "itemId": v.ItemID, "clientId": v.ClientID, "mediaSourceId": v.MediaSourceID, "decision": v.Decision, "state": v.State, "positionMs": v.PositionMS, "startedAt": v.StartedAt, "updatedAt": v.UpdatedAt, "endedAt": v.EndedAt, "stopReason": v.StopReason}
}

func playbackStateDTO(v catalog.PlaybackState) map[string]any {
	return map[string]any{"itemId": v.ItemID, "resumeMs": v.ResumeMS, "playCount": v.PlayCount, "completed": v.Completed, "lastPlayedAt": v.LastPlayedAt, "selectedAudioStreamId": v.SelectedAudioStreamID, "selectedSubtitleStreamId": v.SelectedSubtitleStreamID}
}
