// Package subtitles inspects subtitles without silently rewriting original media.
package subtitles

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Finding struct {
	Token      string `json:"token"`
	Suggestion string `json:"suggestion"`
	Line       int    `json:"line"`
}
type Audit struct {
	Status   string    `json:"status"`
	Cues     int       `json:"cues"`
	SHA256   string    `json:"sha256"`
	OCR      []Finding `json:"ocr"`
	Warnings []string  `json:"warnings"`
}

const maxSRTBytes = 2 << 20

var timestampLine = regexp.MustCompile(`^(\d{2}):(\d{2}):(\d{2}),(\d{3}) --> (\d{2}):(\d{2}):(\d{2}),(\d{3})(?:\s|$)`)
var wordToken = regexp.MustCompile(`[A-Za-z]+(?:['’][A-Za-z]+)?`)

// These are candidate OCR mistakes, not a license to change them without review.
// Proper names and real English words such as Inigo and Impossible are absent.
var suspect = map[string]string{
	"Iove": "love", "Iook": "look", "Ieft": "left", "Ioser": "loser",
	"Iive": "live", "Iife": "life", "Iying": "lying", "Iightning": "lightning",
	"Iegendary": "legendary", "Iifetime": "lifetime", "Iet": "let",
	"Iandmass": "landmass", "Iouder": "louder", "Iettuce": "lettuce",
	"Iiabilities": "liabilities", "Iift": "lift", "Iesson": "lesson",
	"Iwill": "I will", "Iwas": "I was", "Ijust": "I just", "Ido": "I do",
	"Iaccept": "I accept", "Iwould": "I would", "Ihave": "I have",
	"aII": "all", "couId": "could", "wiII": "will", "teII": "tell",
	"peopIe": "people", "wouId": "would", "reaIIy": "really",
	"shouId": "should", "caII": "call", "pIease": "please", "heIp": "help",
	"onIy": "only", "aIways": "always", "aIone": "alone", "IittIe": "little",
	"l'm": "I'm", "l've": "I've", "l'll": "I'll", "l'd": "I'd",
	"lt": "It", "lf": "If", "ls": "Is", "ln": "In",
	"lmpossible": "Impossible", "lmagine": "Imagine", "lndian": "Indian",
	"lnside": "Inside", "lnstead": "Instead", "lnteresting": "Interesting",
}

// AuditSRT validates cue structure and reports suspicious OCR in both directions.
// It never changes, normalizes, or repairs subtitle bytes.
func AuditSRT(data []byte, durationMS int64) (Audit, error) {
	if len(data) == 0 || len(data) > maxSRTBytes || !utf8.Valid(data) {
		return Audit{}, errors.New("invalid SRT encoding or size")
	}
	digest := sha256.Sum256(data)
	report := Audit{Status: "ready", SHA256: hex.EncodeToString(digest[:]), OCR: []Finding{}, Warnings: []string{}}
	lines := strings.Split(strings.TrimPrefix(string(data), "\ufeff"), "\n")
	var lastStart int64 = -1
	var lastEnd int64
	for i, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.Contains(line, "-->") {
			match := timestampLine.FindStringSubmatch(line)
			if match == nil {
				return Audit{}, fmt.Errorf("invalid cue timestamp on line %d", i+1)
			}
			start := clockMS(match[1:5])
			end := clockMS(match[5:9])
			if start >= end {
				return Audit{}, fmt.Errorf("nonpositive cue on line %d", i+1)
			}
			if start < lastStart {
				report.Warnings = append(report.Warnings, "out-of-order cues")
			}
			lastStart = start
			lastEnd = end
			report.Cues++
			continue
		}
		if strings.TrimSpace(line) == "" || strings.TrimSpace(line) == "\ufeff" {
			continue
		}
		// Skip cue numbers and formatting tags, but preserve user text as written.
		if _, err := strconv.Atoi(strings.TrimSpace(line)); err == nil {
			continue
		}
		for _, indices := range wordToken.FindAllStringIndex(line, -1) {
			token := line[indices[0]:indices[1]]
			if corrected, ok := suspect[token]; ok {
				report.OCR = append(report.OCR, Finding{Token: token, Suggestion: corrected, Line: i + 1})
			}
		}
	}
	if report.Cues == 0 {
		return Audit{}, errors.New("no SRT cues")
	}
	if durationMS > 0 && lastEnd > durationMS+30000 {
		report.Warnings = append(report.Warnings, "cues exceed media duration")
	}
	if durationMS > 0 && durationMS-lastEnd > 300000 {
		report.Warnings = append(report.Warnings, "long end-of-media subtitle gap")
	}
	if len(report.OCR) > 0 || len(report.Warnings) > 0 {
		report.Status = "needs_review"
	}
	return report, nil
}
func clockMS(parts []string) int64 {
	nums := make([]int64, len(parts))
	for i, s := range parts {
		nums[i], _ = strconv.ParseInt(s, 10, 64)
	}
	return (((nums[0]*60+nums[1])*60 + nums[2]) * 1000) + nums[3]
}
