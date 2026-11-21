package podcasts

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/atproto"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/reqctx"
)

func (s *Service) transcript(w http.ResponseWriter, r *http.Request) error {
	viewer := reqctx.ViewerDID(r.Context())
	if viewer == "" {
		return httpx.ErrUnauthorized
	}
	id := r.URL.Query().Get("episodeId")
	if id == "" {
		return httpx.ErrNotFound
	}
	episode, err := s.loadEpisode(r.Context(), viewer, id)
	if err != nil {
		return err
	}
	s.hydrateTranscripts(r.Context(), episode)
	visibleEpisode(episode)
	return httpx.OK(w, map[string]any{"episodeId": id, "transcripts": episode["transcripts"]})
}

func (s *Service) hydrateTranscripts(ctx context.Context, episode map[string]any) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	sources, _ := episode["transcripts"].([]any)
	result := make([]any, 0, len(sources))
	for _, source := range sources {
		transcript, ok := source.(map[string]any)
		if !ok {
			continue
		}
		transcript["cues"] = []any{}
		target, _ := transcript["url"].(string)
		if strings.HasPrefix(target, "https://") {
			data, err := atproto.PublicMedia.Fetch(ctx, target, atproto.FetchOptions{MaximumBytes: 8 << 20})
			if err == nil {
				kind, _ := transcript["type"].(string)
				text, cues := ParseTranscript(data, strings.ToLower(kind))
				transcript["text"] = text
				wire := make([]any, 0, len(cues))
				for _, cue := range cues {
					row := map[string]any{"startSeconds": cue.StartSeconds, "text": cue.Text}
					if cue.EndSeconds != nil {
						row["endSeconds"] = *cue.EndSeconds
					}
					wire = append(wire, row)
				}
				transcript["cues"] = wire
			}
		}
		result = append(result, transcript)
	}
	episode["transcripts"] = result
}
