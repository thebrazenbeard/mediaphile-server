package identity

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
)

var (
	moviePattern   = regexp.MustCompile(`^(.+?)\s*\((\d{4})\)(?:\s.*)?$`)
	episodePattern = regexp.MustCompile(`(?i)^(.+?)\s*[-._ ]*S(\d{1,2})E(\d{1,2})(?:\s*[-._ ]+(.*))?$`)
)

type Candidate struct {
	Resolved  bool
	Kind      catalog.ItemKind
	Title     string
	ShowTitle string
	Year      *int
	Season    int
	Episode   int
	Reason    string
}

func Parse(relativePath string, mediaType catalog.LibraryType) Candidate {
	base := strings.TrimSuffix(filepath.Base(relativePath), filepath.Ext(relativePath))
	switch mediaType {
	case catalog.LibraryMovies:
		m := moviePattern.FindStringSubmatch(base)
		if len(m) == 3 {
			year, err := strconv.Atoi(m[2])
			if err == nil {
				return Candidate{Resolved: true, Kind: catalog.ItemMovie, Title: cleanTitle(m[1]), Year: &year}
			}
		}
		return Candidate{Kind: catalog.ItemMovie, Title: cleanTitle(base), Reason: "movie filename lacks an unambiguous Title (YYYY) identity"}
	case catalog.LibraryTV:
		m := episodePattern.FindStringSubmatch(base)
		if len(m) >= 4 {
			season, err1 := strconv.Atoi(m[2])
			episode, err2 := strconv.Atoi(m[3])
			show := cleanTitle(m[1])
			title := ""
			if len(m) >= 5 {
				title = cleanTitle(m[4])
			}
			if title == "" {
				title = "Episode " + strconv.Itoa(episode)
			}
			if err1 == nil && err2 == nil && show != "" {
				return Candidate{Resolved: true, Kind: catalog.ItemEpisode, Title: title, ShowTitle: show, Season: season, Episode: episode}
			}
		}
		return Candidate{Kind: catalog.ItemEpisode, Title: cleanTitle(base), Reason: "TV filename lacks an unambiguous SxxExx identity"}
	default:
		return Candidate{Title: cleanTitle(base), Reason: "unsupported library type"}
	}
}

func cleanTitle(s string) string {
	s = strings.ReplaceAll(s, ".", " ")
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.Trim(s, " -")
	return strings.Join(strings.Fields(s), " ")
}
