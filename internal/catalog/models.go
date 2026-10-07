package catalog

type LibraryType string

const (
	LibraryMovies LibraryType = "movies"
	LibraryTV     LibraryType = "tv"
)

type Library struct {
	ID        string
	Name      string
	MediaType LibraryType
	RootPath  string
	Enabled   bool
}

type ItemKind string

const (
	ItemMovie   ItemKind = "movie"
	ItemShow    ItemKind = "show"
	ItemSeason  ItemKind = "season"
	ItemEpisode ItemKind = "episode"
)

type Item struct {
	ID            string
	LibraryID     string
	ParentID      *string
	Kind          ItemKind
	Title         string
	Year          *int
	SeasonNumber  *int
	EpisodeNumber *int
	Unresolved    bool
}

type MediaSource struct {
	ID         string
	ItemID     string
	EditionID  *string
	Container  string
	DurationMS int64
	Bitrate    int64
	Width      int
	Height     int
	VideoCodec string
	AudioCodec string
	HDR        bool
	Available  bool
}

type MediaPart struct {
	ID        string
	SourceID  string
	Path      string
	Size      int64
	ModTimeNS int64
	Available bool
}

type StreamKind string

const (
	StreamVideo    StreamKind = "video"
	StreamAudio    StreamKind = "audio"
	StreamSubtitle StreamKind = "subtitle"
)

type MediaStream struct {
	ID          string
	PartID      string
	Kind        StreamKind
	StreamIndex int
	Codec       string
	Language    string
	Channels    int
	Width       int
	Height      int
	FrameRate   string
	Default     bool
	Forced      bool
}

type Principal struct {
	ID           string
	Username     string
	PasswordHash string
	Admin        bool
}

type PlaybackState struct {
	PrincipalID              string
	ItemID                   string
	ResumeMS                 int64
	PlayCount                int
	Completed                bool
	LastPlayedAt             *string
	SelectedAudioStreamID    *string
	SelectedSubtitleStreamID *string
}

type PlaybackSession struct {
	ID            string
	PrincipalID   string
	ItemID        string
	ClientID      string
	MediaSourceID *string
	Decision      string
	State         string
	PositionMS    int64
	StartedAt     string
	UpdatedAt     string
	EndedAt       *string
	StopReason    *string
}
