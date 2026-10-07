package playback

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
	"github.com/thebrazenbeard/mediaphile-server/internal/events"
)

var ErrSessionForbidden = errors.New("playback session belongs to another principal")

type SessionManager struct {
	repo   *catalog.Repository
	events *events.Bus
}

func NewSessionManager(repo *catalog.Repository, bus *events.Bus) *SessionManager {
	if bus == nil {
		bus = events.NewBus()
	}
	return &SessionManager{repo: repo, events: bus}
}

func (m *SessionManager) Create(ctx context.Context, principalID, itemID, clientID, sourceID string, mode Mode) (catalog.PlaybackSession, error) {
	source, err := m.repo.GetMediaSource(ctx, sourceID)
	if err != nil {
		return catalog.PlaybackSession{}, err
	}
	if source.ItemID != itemID {
		return catalog.PlaybackSession{}, fmt.Errorf("media source does not belong to item")
	}
	id, err := sessionID()
	if err != nil {
		return catalog.PlaybackSession{}, err
	}
	sourceCopy := sourceID
	v := catalog.PlaybackSession{ID: id, PrincipalID: principalID, ItemID: itemID, ClientID: clientID, MediaSourceID: &sourceCopy, Decision: string(mode), State: "playing"}
	if err := m.repo.CreatePlaybackSession(ctx, v); err != nil {
		return catalog.PlaybackSession{}, err
	}
	stored, err := m.repo.GetPlaybackSession(ctx, id)
	if err != nil {
		return catalog.PlaybackSession{}, err
	}
	m.events.Publish(events.Event{Type: "media.play", PrincipalID: principalID, ClientID: clientID, ItemID: itemID, SessionID: id})
	return stored, nil
}

func (m *SessionManager) Update(ctx context.Context, principalID, sessionID string, positionMS int64, state string) (catalog.PlaybackSession, error) {
	current, err := m.repo.GetPlaybackSession(ctx, sessionID)
	if err != nil {
		return catalog.PlaybackSession{}, err
	}
	if current.PrincipalID != principalID {
		return catalog.PlaybackSession{}, ErrSessionForbidden
	}
	if positionMS < 0 {
		positionMS = 0
	}
	var duration int64
	if current.MediaSourceID != nil {
		source, err := m.repo.GetMediaSource(ctx, *current.MediaSourceID)
		if err == nil {
			duration = source.DurationMS
		}
	}
	pb, err := m.repo.GetPlaybackState(ctx, principalID, current.ItemID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return catalog.PlaybackSession{}, err
	}
	if errors.Is(err, sql.ErrNoRows) {
		pb = catalog.PlaybackState{PrincipalID: principalID, ItemID: current.ItemID}
	}
	wasCompleted := pb.Completed
	reached := duration > 0 && positionMS*100 >= duration*90
	pb.Completed = pb.Completed || reached
	if reached {
		pb.ResumeMS = 0
		if !wasCompleted {
			pb.PlayCount++
		}
	} else if !pb.Completed {
		pb.ResumeMS = positionMS
	}
	updated, err := m.repo.UpdateSessionAndPlaybackState(ctx, sessionID, positionMS, state, pb)
	if err != nil {
		return catalog.PlaybackSession{}, err
	}
	eventType := "media.progress"
	switch state {
	case "paused":
		eventType = "media.pause"
	case "resumed":
		eventType = "media.resume"
	}
	m.events.Publish(events.Event{Type: eventType, PrincipalID: principalID, ClientID: current.ClientID, ItemID: current.ItemID, SessionID: sessionID, PositionMS: positionMS})
	if reached && !wasCompleted {
		m.events.Publish(events.Event{Type: "media.completed", PrincipalID: principalID, ClientID: current.ClientID, ItemID: current.ItemID, SessionID: sessionID, PositionMS: positionMS})
	}
	return updated, nil
}

func (m *SessionManager) End(ctx context.Context, principalID, sessionID, reason string) error {
	current, err := m.repo.GetPlaybackSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if current.PrincipalID != principalID {
		return ErrSessionForbidden
	}
	if reason == "" {
		reason = "stopped"
	}
	if err := m.repo.EndPlaybackSession(ctx, sessionID, reason); err != nil {
		return err
	}
	m.events.Publish(events.Event{Type: "media.stop", PrincipalID: principalID, ClientID: current.ClientID, ItemID: current.ItemID, SessionID: sessionID, PositionMS: current.PositionMS})
	return nil
}

func (m *SessionManager) PlaybackState(ctx context.Context, principalID, itemID string) (catalog.PlaybackState, error) {
	return m.repo.GetPlaybackState(ctx, principalID, itemID)
}

func sessionID() (string, error) {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "play_" + base64.RawURLEncoding.EncodeToString(buf), nil
}
