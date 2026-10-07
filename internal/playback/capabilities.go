package playback

type Capabilities struct {
	ClientID        string   `json:"clientId"`
	Containers      []string `json:"containers"`
	VideoCodecs     []string `json:"videoCodecs"`
	AudioCodecs     []string `json:"audioCodecs"`
	SubtitleCodecs  []string `json:"subtitleCodecs"`
	MaxWidth        int      `json:"maxWidth"`
	MaxHeight       int      `json:"maxHeight"`
	MaxVideoBitrate int64    `json:"maxVideoBitrate"`
	HLS             bool     `json:"hls"`
	RangeRequests   bool     `json:"rangeRequests"`
}

type Selection struct {
	AudioStreamID    string `json:"audioStreamId,omitempty"`
	SubtitleStreamID string `json:"subtitleStreamId,omitempty"`
}

type Stream struct {
	ID    string
	Kind  string
	Codec string
}

type Source struct {
	ID         string
	PartID     string
	Container  string
	VideoCodec string
	AudioCodec string
	Width      int
	Height     int
	Bitrate    int64
	Available  bool
	Streams    []Stream
}
