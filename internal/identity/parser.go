package identity

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
)

var (
	moviePattern             = regexp.MustCompile(`^(.+?)\s*\((\d{4})\)(?:\s.*)?$`)
	episodePattern           = regexp.MustCompile(`(?i)^(.+?)\s*[-._ ]*S(\d{1,2})E(\d{1,2})(?:\s*[-._ ]+(.*))?$`)
	bareEpisodePattern       = regexp.MustCompile(`(?i)^S(\d{1,2})E(\d{1,2})(?:\s*[-._ ]+(.*))?$`)
	seasonFolderPattern      = regexp.MustCompile(`(?i)^Season\s*(\d{1,2})$`)
	namedSeasonFolderPattern = regexp.MustCompile(`(?i)^(.+?)\s*[-._ ]+Season\s*(\d{1,2})$`)
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
		// Historical libraries may use SxxExx-only basenames. Only a
		// sufficiently specific parent directory can supply a show identity.
		if m := bareEpisodePattern.FindStringSubmatch(base); len(m) >= 3 {
			season, err1 := strconv.Atoi(m[1])
			episode, err2 := strconv.Atoi(m[2])
			show := showFromEpisodeDirectory(relativePath, season)
			if err1 == nil && err2 == nil && show != "" {
				title := "Episode " + strconv.Itoa(episode)
				if len(m) >= 4 && cleanTitle(m[3]) != "" {
					title = cleanTitle(m[3])
				}
				return Candidate{Resolved: true, Kind: catalog.ItemEpisode, Title: title, ShowTitle: show, Season: season, Episode: episode}
			}
		}
		return Candidate{Kind: catalog.ItemEpisode, Title: cleanTitle(base), Reason: "TV filename lacks an unambiguous SxxExx identity"}
	default:
		return Candidate{Title: cleanTitle(base), Reason: "unsupported library type"}
	}
}

func showFromEpisodeDirectory(relativePath string, season int) string {
	dir := filepath.Dir(relativePath)
	if dir == "." {
		return ""
	}
	folder := filepath.Base(dir)
	if m := namedSeasonFolderPattern.FindStringSubmatch(folder); len(m) == 3 {
		if filepath.Dir(dir) != "." {
			return ""
		}
		n, _ := strconv.Atoi(m[2])
		if n != season {
			return ""
		}
		return cleanTitle(m[1])
	}
	if m := seasonFolderPattern.FindStringSubmatch(folder); len(m) == 2 {
		n, _ := strconv.Atoi(m[1])
		if n != season {
			return ""
		}
		dir = filepath.Dir(dir)
		if dir == "." {
			return ""
		}
		if filepath.Dir(dir) != "." {
			// Only Show/Season NN, not Show/Extras/Season NN.
			return ""
		}
		folder = filepath.Base(dir)
	} else if filepath.Dir(dir) != "." {
		// A nested leaf such as Show/Extras must not masquerade as a show.
		return ""
	}
	return cleanTitle(folder)
}

func cleanTitle(s string) string {
	s = strings.ReplaceAll(s, ".", " ")
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.Trim(s, " -")
	return strings.Join(strings.Fields(s), " ")
}
