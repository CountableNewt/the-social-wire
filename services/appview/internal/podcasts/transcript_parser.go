package podcasts

import (
	"encoding/json"
	"html"
	"regexp"
	"strconv"
	"strings"
)

func ParseTranscript(data []byte, mime string) (string, []TranscriptCue) {
	raw := strings.ToValidUTF8(string(data), "�")
	cues := []TranscriptCue{}
	if strings.Contains(mime, "json") {
		var value any
		if json.Unmarshal(data, &value) == nil {
			if obj, ok := value.(map[string]any); ok {
				value = obj["segments"]
			}
			items, _ := value.([]any)
			for _, v := range items {
				item, ok := v.(map[string]any)
				if !ok {
					continue
				}
				text, hasText := item["text"].(string)
				start := rowNumber(item, "startTime", "start", "startSeconds")
				if hasText && start != nil {
					cues = append(cues, TranscriptCue{StartSeconds: *start, EndSeconds: rowNumber(item, "endTime", "end", "endSeconds"), Text: text})
				}
			}
			return cueText(cues), cues
		}
	}
	if strings.Contains(mime, "vtt") || strings.Contains(mime, "srt") || strings.Contains(mime, "subrip") {
		for _, block := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n\n") {
			lines := strings.Split(block, "\n")
			for i, line := range lines {
				if !strings.Contains(line, "-->") {
					continue
				}
				parts := strings.Split(line, "-->")
				if len(parts) == 2 {
					start, end := transcriptTime(parts[0]), transcriptTime(parts[1])
					if start != nil && end != nil {
						text := transcriptPlainText(strings.Join(lines[i+1:], "\n"))
						if text != "" {
							cues = append(cues, TranscriptCue{StartSeconds: *start, EndSeconds: end, Text: text})
						}
					}
				}
				break
			}
		}
		return cueText(cues), cues
	}
	if strings.Contains(mime, "html") {
		raw = transcriptPlainText(raw)
	}
	return raw, cues
}
func rowNumber(row map[string]any, keys ...string) *float64 {
	for _, key := range keys {
		if raw, ok := row[key]; ok {
			if n, ok := raw.(float64); ok {
				return &n
			}
			return nil
		}
	}
	return nil
}
func cueText(cues []TranscriptCue) string {
	text := make([]string, len(cues))
	for i, c := range cues {
		text[i] = c.Text
	}
	return strings.Join(text, "\n")
}
func transcriptTime(raw string) *float64 {
	parts := strings.Split(strings.TrimSpace(raw), " ")
	raw = strings.ReplaceAll(parts[0], ",", ".")
	var total float64
	for _, part := range strings.Split(raw, ":") {
		value, err := strconv.ParseFloat(part, 64)
		if err != nil || value < 0 {
			return nil
		}
		total = total*60 + value
	}
	return &total
}

var transcriptTag = regexp.MustCompile(`<[^>]*>`)

func transcriptPlainText(value string) string {
	return strings.TrimSpace(html.UnescapeString(transcriptTag.ReplaceAllString(value, "")))
}
