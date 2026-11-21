package podcasts

type TranscriptCue struct {
	StartSeconds float64  `json:"startSeconds"`
	EndSeconds   *float64 `json:"endSeconds,omitempty"`
	Text         string   `json:"text"`
}
