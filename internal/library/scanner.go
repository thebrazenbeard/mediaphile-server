package library

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
	"github.com/thebrazenbeard/mediaphile-server/internal/identity"
	"github.com/thebrazenbeard/mediaphile-server/internal/probe"
)

type Scanner struct {
	repo   *catalog.Repository
	prober probe.Prober
}

type ScanResult struct {
	Scanned    int
	Probed     int
	Unchanged  int
	Missing    int
	Unresolved int
}

func NewScanner(repo *catalog.Repository, prober probe.Prober) *Scanner {
	return &Scanner{repo: repo, prober: prober}
}

func (s *Scanner) Scan(ctx context.Context, libraryID string) (ScanResult, error) {
	lib, err := s.repo.GetLibrary(ctx, libraryID)
	if err != nil {
		return ScanResult{}, err
	}
	if !lib.Enabled {
		return ScanResult{}, fmt.Errorf("library %s is disabled", libraryID)
	}

	root, err := filepath.Abs(lib.RootPath)
	if err != nil {
		return ScanResult{}, err
	}
	seen := map[string]bool{}
	var result ScanResult
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&fs.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !supportedMedia(path) {
			return nil
		}
		result.Scanned++
		clean, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, clean)
		if err != nil {
			return err
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("media path escapes library root: %s", path)
		}
		seen[filepath.Clean(clean)] = true
		info, err := d.Info()
		if err != nil {
			return err
		}
		existing, findErr := s.repo.FindMediaPartByPath(ctx, clean)
		if findErr == nil && existing.Size == info.Size() && existing.ModTimeNS == info.ModTime().UnixNano() && existing.Available {
			result.Unchanged++
			return nil
		}
		if findErr != nil && !catalog.IsNotFound(findErr) {
			return findErr
		}

		media, err := s.prober.Probe(ctx, clean)
		if err != nil {
			return err
		}
		result.Probed++
		candidate := identity.Parse(rel, lib.MediaType)
		if !candidate.Resolved {
			result.Unresolved++
		}
		itemID, err := s.ensureIdentity(ctx, lib, rel, candidate)
		if err != nil {
			return err
		}

		sourceID := stableID("source", libraryID, strings.ToLower(filepath.Clean(clean)))
		partID := stableID("part", libraryID, strings.ToLower(filepath.Clean(clean)))
		if err := s.repo.UpsertMediaSource(ctx, catalog.MediaSource{
			ID: sourceID, ItemID: itemID, Container: media.Container, DurationMS: media.DurationMS, Bitrate: media.Bitrate,
			Width: media.Width, Height: media.Height, VideoCodec: media.VideoCodec, AudioCodec: media.AudioCodec, HDR: media.HDR, Available: true,
		}); err != nil {
			return err
		}
		if err := s.repo.UpsertMediaPart(ctx, catalog.MediaPart{ID: partID, SourceID: sourceID, Path: clean, Size: info.Size(), ModTimeNS: info.ModTime().UnixNano(), Available: true}); err != nil {
			return err
		}
		if err := s.repo.DeleteStreamsForPart(ctx, partID); err != nil {
			return err
		}
		for _, stream := range media.Streams {
			kind := catalog.StreamKind(stream.Kind)
			if err := s.repo.UpsertMediaStream(ctx, catalog.MediaStream{
				ID: stableID("stream", partID, strconv.Itoa(stream.Index)), PartID: partID, Kind: kind, StreamIndex: stream.Index,
				Codec: stream.Codec, Language: stream.Language, Channels: stream.Channels, Width: stream.Width, Height: stream.Height,
				FrameRate: stream.FrameRate, Default: stream.Default, Forced: stream.Forced,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return result, err
	}

	parts, err := s.repo.ListPartsByLibrary(ctx, libraryID)
	if err != nil {
		return result, err
	}
	for _, part := range parts {
		if !seen[filepath.Clean(part.Path)] && part.Available {
			if err := s.repo.SetMediaPartAvailable(ctx, part.ID, false); err != nil {
				return result, err
			}
			result.Missing++
		}
	}
	return result, nil
}

func (s *Scanner) ensureIdentity(ctx context.Context, lib catalog.Library, rel string, c identity.Candidate) (string, error) {
	if !c.Resolved {
		id := stableID("item", lib.ID, "unresolved", strings.ToLower(filepath.ToSlash(rel)))
		return id, s.repo.UpsertItem(ctx, catalog.Item{ID: id, LibraryID: lib.ID, Kind: c.Kind, Title: c.Title, Unresolved: true})
	}
	if c.Kind == catalog.ItemMovie {
		year := 0
		if c.Year != nil {
			year = *c.Year
		}
		id := stableID("movie", lib.ID, strings.ToLower(c.Title), strconv.Itoa(year))
		return id, s.repo.UpsertItem(ctx, catalog.Item{ID: id, LibraryID: lib.ID, Kind: catalog.ItemMovie, Title: c.Title, Year: c.Year})
	}
	showID := stableID("show", lib.ID, strings.ToLower(c.ShowTitle))
	if err := s.repo.UpsertItem(ctx, catalog.Item{ID: showID, LibraryID: lib.ID, Kind: catalog.ItemShow, Title: c.ShowTitle}); err != nil {
		return "", err
	}
	seasonID := stableID("season", showID, strconv.Itoa(c.Season))
	season := c.Season
	if err := s.repo.UpsertItem(ctx, catalog.Item{ID: seasonID, LibraryID: lib.ID, ParentID: &showID, Kind: catalog.ItemSeason, Title: fmt.Sprintf("Season %d", c.Season), SeasonNumber: &season}); err != nil {
		return "", err
	}
	episodeID := stableID("episode", seasonID, strconv.Itoa(c.Episode))
	episode := c.Episode
	if err := s.repo.UpsertItem(ctx, catalog.Item{ID: episodeID, LibraryID: lib.ID, ParentID: &seasonID, Kind: catalog.ItemEpisode, Title: c.Title, SeasonNumber: &season, EpisodeNumber: &episode}); err != nil {
		return "", err
	}
	return episodeID, nil
}

func supportedMedia(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mkv", ".mp4", ".m4v", ".avi", ".mov", ".webm":
		return true
	default:
		return false
	}
}

func stableID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf("%s-%x", parts[0], sum[:8])
}
