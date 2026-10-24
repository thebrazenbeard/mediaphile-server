package subtitles

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Candidate struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Language string `json:"language"`
	Bytes    int64  `json:"bytes"`
	Audit    Audit  `json:"audit"`
}
type SidecarAudit struct {
	Name     string `json:"name"`
	Language string `json:"language"`
	Status   string `json:"status"`
	Audit    *Audit `json:"audit,omitempty"`
	Reason   string `json:"reason,omitempty"`
}
type Review struct {
	EnglishEmbedded bool           `json:"englishEmbedded"`
	Existing        []string       `json:"existing"`
	ExistingAudits  []SidecarAudit `json:"existingAudits"`
	Candidates      []Candidate    `json:"candidates"`
	Coverage        string         `json:"coverage"`
}
type Installed struct {
	Destination string `json:"destination"`
	SHA256      string `json:"sha256"`
	Bytes       int64  `json:"bytes"`
	Source      string `json:"source"`
}

func languageFor(name, stem string) string {
	prefix := strings.ToLower(stem)
	s := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
	switch {
	case s == prefix:
		return "und"
	case s == prefix+".en" || s == prefix+".eng" || s == prefix+".english":
		return "en"
	default:
		return ""
	}
}

// Discover searches adjacent SRT files and a strictly bounded Subtitles/Subs inbox.
// Unlabeled and foreign-language subtitles are never considered verified English.
func Discover(video string, durationMS int64, embeddedEnglish bool) (Review, error) {
	info, err := os.Lstat(video)
	if err != nil {
		return Review{}, err
	}
	if !info.Mode().IsRegular() {
		return Review{}, errors.New("video is not a regular file")
	}
	base := filepath.Base(video)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	dir := filepath.Dir(video)
	report := Review{EnglishEmbedded: embeddedEnglish, Existing: []string{}, ExistingAudits: []SidecarAudit{}, Candidates: []Candidate{}, Coverage: "missing"}
	if embeddedEnglish {
		report.Coverage = "embedded_english"
	}
	for _, folder := range []string{dir, filepath.Join(dir, "Subtitles"), filepath.Join(dir, "Subs")} {
		names, err := os.ReadDir(folder)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return Review{}, err
		}
		for _, entry := range names {
			if !strings.EqualFold(filepath.Ext(entry.Name()), ".srt") {
				continue
			}
			language := languageFor(entry.Name(), stem)
			if language == "" {
				continue
			}
			if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
				continue
			}
			path := filepath.Join(folder, entry.Name())
			stat, err := entry.Info()
			if err != nil {
				return Review{}, err
			}
			if stat.Size() > maxSRTBytes || stat.Size() == 0 {
				continue
			}
			if folder == dir {
				report.Existing = append(report.Existing, entry.Name())
				row := SidecarAudit{Name: entry.Name(), Language: language, Status: "unverified"}
				if language == "en" {
					report.Coverage = "external_english"
					data, readErr := os.ReadFile(path)
					if readErr != nil {
						row.Reason = "could not read existing subtitle"
					} else if audit, auditErr := AuditSRT(data, durationMS); auditErr == nil {
						row.Audit = &audit
						row.Status = audit.Status
					} else {
						row.Reason = "existing English subtitle failed SRT validation"
					}
				} else {
					row.Reason = "unlabeled subtitle language requires review"
				}
				report.ExistingAudits = append(report.ExistingAudits, row)
				continue
			}
			if language != "en" {
				continue
			}
			bytes, err := os.ReadFile(path)
			if err != nil {
				return Review{}, err
			}
			audit, err := AuditSRT(bytes, durationMS)
			if err != nil {
				continue
			}
			sum := sha256.Sum256([]byte(filepath.Clean(path)))
			report.Candidates = append(report.Candidates, Candidate{
				ID: hex.EncodeToString(sum[:16]), Name: entry.Name(), Language: "en", Bytes: stat.Size(), Audit: audit,
			})
		}
	}
	return report, nil
}

// Install copies a reviewed local candidate to a new English sidecar. No originals,
// installed subtitles, or videos are ever overwritten.
func Install(video string, durationMS int64, candidateID string) (Installed, error) {
	if len(candidateID) != 32 {
		return Installed{}, errors.New("invalid candidate identifier")
	}
	review, err := Discover(video, durationMS, false)
	if err != nil {
		return Installed{}, err
	}
	if review.Coverage == "external_english" {
		return Installed{}, errors.New("English sidecar already present")
	}
	var candidate Candidate
	found := false
	for _, c := range review.Candidates {
		if c.ID == candidateID {
			candidate = c
			found = true
			break
		}
	}
	if !found {
		return Installed{}, errors.New("candidate not found beside video")
	}
	if candidate.Audit.Status != "ready" {
		return Installed{}, errors.New("candidate has unresolved quality flags")
	}
	source := ""
	for _, folder := range []string{"Subtitles", "Subs"} {
		possible := filepath.Join(filepath.Dir(video), folder, candidate.Name)
		sum := sha256.Sum256([]byte(filepath.Clean(possible)))
		if hex.EncodeToString(sum[:16]) == candidate.ID {
			source = possible
			break
		}
	}
	if source == "" {
		return Installed{}, errors.New("candidate path rejected")
	}
	info, err := os.Lstat(source)
	if err != nil {
		return Installed{}, err
	}
	if !info.Mode().IsRegular() || info.Size() != candidate.Bytes {
		return Installed{}, errors.New("candidate changed")
	}
	payload, err := os.ReadFile(source)
	if err != nil {
		return Installed{}, err
	}
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])
	if int64(len(payload)) != candidate.Bytes || digest != candidate.Audit.SHA256 {
		return Installed{}, errors.New("candidate changed before installation")
	}
	destination := strings.TrimSuffix(video, filepath.Ext(video)) + ".en.srt"
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return Installed{}, err
	}
	n, writeErr := out.Write(payload)
	closeErr := out.Close()
	if writeErr != nil {
		return Installed{}, writeErr
	}
	if closeErr != nil || n != len(payload) {
		return Installed{}, errors.New("new sidecar write incomplete; inspect exclusive-created destination")
	}
	readback, err := os.ReadFile(destination)
	if err != nil {
		return Installed{}, err
	}
	actual := sha256.Sum256(readback)
	if hex.EncodeToString(actual[:]) != digest {
		return Installed{}, errors.New("installed SHA256 mismatch")
	}
	return Installed{Destination: destination, SHA256: digest, Bytes: int64(n), Source: candidate.Name}, nil
}
