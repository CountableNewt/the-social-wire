package drafts

import (
	"encoding/json"
	"reflect"
	"strings"
	"time"
)

type asset struct {
	ID          string `json:"assetId"`
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	SHA256      string `json:"sha256"`
	Kind        string `json:"kind"`
	State       string `json:"state"`
	ByteLength  int64  `json:"byteLength"`
}

type articleAsset struct {
	ID       string `json:"id"`
	Alt      string `json:"alt"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	MIMEType string `json:"mimeType"`
	Name     string `json:"name"`
}

type reference struct {
	LocalID string `json:"localAssetId"`
	AssetID string `json:"assetId"`
}

type binding struct {
	LocalID string `json:"localAssetId"`
	Asset   asset  `json:"asset"`
}

type articleDocument struct {
	ID          string          `json:"draftId"`
	Revision    int64           `json:"revision"`
	Deleted     bool            `json:"deleted"`
	MutationID  string          `json:"mutationId"`
	Updated     string          `json:"updatedAt"`
	Draft       json.RawMessage `json:"draft,omitempty"`
	Attachments []binding       `json:"attachments"`
}

type articlePayload struct {
	ID               string         `json:"id"`
	Title            string         `json:"title"`
	Markdown         string         `json:"markdown"`
	Excerpt          string         `json:"excerpt"`
	Path             string         `json:"path"`
	PublicationURI   string         `json:"publicationUri"`
	Created          string         `json:"createdAt"`
	Updated          string         `json:"updatedAt"`
	Tags             []string       `json:"tags"`
	Assets           []articleAsset `json:"assets"`
	Cover            *string        `json:"coverAssetId"`
	Published        *string        `json:"publishedUri"`
	ScheduleRecord   *string        `json:"scheduleRecordKey"`
	ScheduleURI      *string        `json:"scheduleUri"`
	PublishingRecord *string        `json:"publishingRecordKey"`
	Claim            *string        `json:"publicationClaimId"`
}

func (p articlePayload) locked() bool {
	return p.Published != nil || p.ScheduleRecord != nil || p.PublishingRecord != nil
}

func (p articlePayload) sameContent(q articlePayload) bool {
	p.Updated, q.Updated = "", ""
	p.Published, p.ScheduleRecord, p.ScheduleURI, p.PublishingRecord, p.Claim = nil, nil, nil, nil, nil
	q.Published, q.ScheduleRecord, q.ScheduleURI, q.PublishingRecord, q.Claim = nil, nil, nil, nil, nil
	return reflect.DeepEqual(p, q)
}

func (p articlePayload) valid(refs []reference) bool {
	if !uuid.MatchString(p.ID) || len(p.Tags) > 100 || p.Tags == nil || p.Assets == nil || len(p.Assets) > 200 ||
		(p.Claim != nil && !uuid.MatchString(*p.Claim)) || (p.PublishingRecord != nil && p.Claim == nil) || len(refs) != len(p.Assets) {
		return false
	}
	for _, date := range []string{p.Created, p.Updated} {
		if _, err := time.Parse(time.RFC3339Nano, date); err != nil {
			return false
		}
	}
	for _, text := range []string{p.Title, p.Markdown, p.Excerpt, p.Path, p.PublicationURI} {
		if strings.ContainsRune(text, 0) {
			return false
		}
	}
	for _, tag := range p.Tags {
		if len(tag) > 256 {
			return false
		}
	}
	for _, value := range []*string{p.Published, p.ScheduleRecord, p.ScheduleURI, p.PublishingRecord} {
		if value != nil && (*value == "" || len(*value) > 4096 || strings.ContainsRune(*value, 0)) {
			return false
		}
	}
	assets := map[string]bool{}
	for _, descriptor := range p.Assets {
		if assets[descriptor.ID] || !uuid.MatchString(descriptor.ID) || descriptor.Width <= 0 || descriptor.Width > 100000 ||
			descriptor.Height <= 0 || descriptor.Height > 100000 || descriptor.Name == "" || len(descriptor.Name) > 1024 ||
			strings.ContainsRune(descriptor.Name, 0) || strings.ContainsRune(descriptor.Alt, 0) {
			return false
		}
		switch descriptor.MIMEType {
		case "image/jpeg", "image/png", "image/gif", "image/webp":
		default:
			return false
		}
		assets[descriptor.ID] = true
	}
	if p.Cover != nil && !assets[*p.Cover] {
		return false
	}
	for _, ref := range refs {
		if !assets[ref.LocalID] || !uuid.MatchString(ref.AssetID) {
			return false
		}
		delete(assets, ref.LocalID)
	}
	return true
}
