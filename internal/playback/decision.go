package playback

import (
	"sort"
	"strings"
)

type Mode string

const (
	DirectPlay Mode = "DIRECT_PLAY"
	Remux      Mode = "REMUX"
	Transcode  Mode = "TRANSCODE"
	Unplayable Mode = "UNPLAYABLE"
)

type Decision struct {
	Mode             Mode     `json:"mode"`
	SourceID         string   `json:"sourceId,omitempty"`
	PartID           string   `json:"partId,omitempty"`
	AudioStreamID    string   `json:"audioStreamId,omitempty"`
	SubtitleStreamID string   `json:"subtitleStreamId,omitempty"`
	URL              string   `json:"url,omitempty"`
	Reasons          []string `json:"reasons"`
}

type candidate struct {
	decision Decision
	cost     int
}

func Decide(input []Source, caps Capabilities, selection Selection, ffmpegAvailable bool) Decision {
	sources := append([]Source(nil), input...)
	sort.Slice(sources, func(i, j int) bool { return sources[i].ID < sources[j].ID })
	var best *candidate
	available := false
	for _, source := range sources {
		if !source.Available || source.PartID == "" {
			continue
		}
		available = true
		c := evaluate(source, caps, selection, ffmpegAvailable)
		if c.decision.Mode == Unplayable {
			continue
		}
		if best == nil || c.cost < best.cost {
			copy := c
			best = &copy
		}
	}
	if best != nil {
		return best.decision
	}
	if !available {
		return Decision{Mode: Unplayable, Reasons: []string{"NO_ACCESSIBLE_SOURCE"}}
	}
	return Decision{Mode: Unplayable, Reasons: []string{"NO_TRANSFORM_CAPABILITY"}}
}

func evaluate(source Source, caps Capabilities, selection Selection, ffmpegAvailable bool) candidate {
	reasons := []string{}
	containerOK := containsFold(caps.Containers, source.Container)
	videoOK := source.VideoCodec == "" || containsFold(caps.VideoCodecs, source.VideoCodec)
	audioOK := source.AudioCodec == "" || containsFold(caps.AudioCodecs, source.AudioCodec)
	if !containerOK {
		reasons = append(reasons, "CONTAINER_UNSUPPORTED")
	}
	if !videoOK {
		reasons = append(reasons, "VIDEO_CODEC_UNSUPPORTED")
	}
	if !audioOK {
		reasons = append(reasons, "AUDIO_CODEC_UNSUPPORTED")
	}
	if caps.MaxWidth > 0 && source.Width > caps.MaxWidth {
		reasons = append(reasons, "RESOLUTION_LIMIT")
	}
	if caps.MaxHeight > 0 && source.Height > caps.MaxHeight && !hasReason(reasons, "RESOLUTION_LIMIT") {
		reasons = append(reasons, "RESOLUTION_LIMIT")
	}
	if caps.MaxVideoBitrate > 0 && source.Bitrate > caps.MaxVideoBitrate {
		reasons = append(reasons, "BITRATE_LIMIT")
	}

	if selection.SubtitleStreamID != "" {
		found := false
		for _, stream := range source.Streams {
			if stream.ID == selection.SubtitleStreamID && stream.Kind == "subtitle" {
				found = true
				if !containsFold(caps.SubtitleCodecs, stream.Codec) {
					reasons = append(reasons, "SUBTITLE_BURN_REQUIRED")
				}
				break
			}
		}
		if !found {
			reasons = append(reasons, "SUBTITLE_NOT_FOUND")
		}
	}
	if selection.AudioStreamID != "" {
		found := false
		for _, stream := range source.Streams {
			if stream.ID == selection.AudioStreamID && stream.Kind == "audio" {
				found = true
				break
			}
		}
		if !found {
			reasons = append(reasons, "AUDIO_STREAM_NOT_FOUND")
		}
	}

	if len(reasons) == 0 {
		return candidate{cost: 0, decision: Decision{Mode: DirectPlay, SourceID: source.ID, PartID: source.PartID, AudioStreamID: selection.AudioStreamID, SubtitleStreamID: selection.SubtitleStreamID, URL: "/api/v1/media/" + source.PartID + "/content", Reasons: []string{}}}
	}
	onlyContainer := len(reasons) == 1 && reasons[0] == "CONTAINER_UNSUPPORTED"
	if !ffmpegAvailable {
		return candidate{cost: 9, decision: Decision{Mode: Unplayable, SourceID: source.ID, PartID: source.PartID, Reasons: append(reasons, "NO_TRANSFORM_CAPABILITY")}}
	}
	if onlyContainer {
		return candidate{cost: 1, decision: Decision{Mode: Remux, SourceID: source.ID, PartID: source.PartID, AudioStreamID: selection.AudioStreamID, SubtitleStreamID: selection.SubtitleStreamID, URL: "/api/v1/transcode/{sessionId}/master.m3u8", Reasons: reasons}}
	}
	return candidate{cost: 2, decision: Decision{Mode: Transcode, SourceID: source.ID, PartID: source.PartID, AudioStreamID: selection.AudioStreamID, SubtitleStreamID: selection.SubtitleStreamID, URL: "/api/v1/transcode/{sessionId}/master.m3u8", Reasons: reasons}}
}

func containsFold(values []string, want string) bool {
	for _, v := range values {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}

func hasReason(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
