package richtext

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

type markdownFixture struct {
	Profile string `json:"profile"`
	Cases   []struct {
		Name          string        `json:"name"`
		Source        string        `json:"source"`
		Text          string        `json:"text"`
		GraphemeCount *int          `json:"graphemeCount"`
		Valid         *bool         `json:"valid"`
		Mentions      []Mention     `json:"mentions"`
		Facets        []goldenFacet `json:"facets"`
	} `json:"cases"`
}

type goldenFacet struct {
	ByteStart int    `json:"byteStart"`
	ByteEnd   int    `json:"byteEnd"`
	Type      string `json:"type"`
	Value     string `json:"value"`
}

func TestSocialMarkdownMatchesSharedGoldenCorpus(t *testing.T) {
	data, err := os.ReadFile("testdata/social-markdown.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture markdownFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Profile != "skej.social-markdown.v1" {
		t.Fatalf("profile %q", fixture.Profile)
	}
	for _, testCase := range fixture.Cases {
		t.Run(testCase.Name, func(t *testing.T) {
			compilation := Compile(testCase.Source, testCase.Mentions)
			if compilation.Text != testCase.Text {
				t.Errorf("text %q, want %q", compilation.Text, testCase.Text)
			}
			got := []goldenFacet{}
			for _, facet := range compilation.Facets {
				if golden, ok := toGolden(facet); ok {
					got = append(got, golden)
				}
			}
			want := testCase.Facets
			if want == nil {
				want = []goldenFacet{}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("facets %+v, want %+v", got, want)
			}
			if testCase.GraphemeCount != nil {
				count := GraphemeCount(compilation.Text)
				if count != *testCase.GraphemeCount {
					t.Errorf("count %d, want %d", count, *testCase.GraphemeCount)
				}
				if testCase.Valid == nil || *testCase.Valid != (count <= 300) {
					t.Errorf("validity mismatch for %d", count)
				}
			}
		})
	}
}

func toGolden(value any) (goldenFacet, bool) {
	object, _ := value.(map[string]any)
	index, _ := object["index"].(map[string]any)
	start, ok1 := index["byteStart"].(float64)
	end, ok2 := index["byteEnd"].(float64)
	features, _ := object["features"].([]any)
	if !ok1 || !ok2 || len(features) == 0 {
		return goldenFacet{}, false
	}
	feature, _ := features[0].(map[string]any)
	facet := goldenFacet{ByteStart: int(start), ByteEnd: int(end)}
	switch feature["$type"] {
	case "app.bsky.richtext.facet#link":
		facet.Type, facet.Value = "link", feature["uri"].(string)
	case "app.bsky.richtext.facet#tag":
		facet.Type, facet.Value = "tag", feature["tag"].(string)
	case "app.bsky.richtext.facet#mention":
		facet.Type, facet.Value = "mention", feature["did"].(string)
	default:
		return goldenFacet{}, false
	}
	return facet, true
}

func TestCanonicalFacetsDetectLinksAndTagsAndValidateExplicitOnes(t *testing.T) {
	text := "Read https://example.com/docs). #skej"
	facets := Facets(text, nil, nil, true)
	if len(facets) != 2 {
		t.Fatalf("facets %v", facets)
	}
	link, _ := toGolden(facets[0])
	tag, _ := toGolden(facets[1])
	if link != (goldenFacet{5, 29, "link", "https://example.com/docs"}) || tag != (goldenFacet{32, 37, "tag", "skej"}) {
		t.Fatalf("link %+v tag %+v", link, tag)
	}
	if err := ValidateFacets(facets, text); err != nil {
		t.Fatal(err)
	}
	shifted := []any{facetValue(byteRange{6, 29}, map[string]any{"$type": "app.bsky.richtext.facet#link", "uri": "https://example.com/docs"})}
	if err := ValidateFacets(shifted, text); err != ErrInvalidFacet {
		t.Fatalf("expected invalid facet, got %v", err)
	}
	if Facets("Plain", []any{}, nil, true) != nil {
		t.Fatal("explicit empty facets must suppress detection")
	}
}

func TestNormalizeEmbedAddsExternalType(t *testing.T) {
	embed := NormalizeEmbed(map[string]any{"external": map[string]any{"uri": "https://example.com/x"}})
	want := map[string]any{"$type": "app.bsky.embed.external", "external": map[string]any{
		"uri": "https://example.com/x", "title": "example.com", "description": "",
	}}
	if !reflect.DeepEqual(embed, want) {
		t.Fatalf("%v", embed)
	}
}

func TestGraphemeCountsEmojiSequencesAsOneCharacter(t *testing.T) {
	for _, value := range []string{"\U0001F44B\U0001F3FD", "\U0001F468\u200D\U0001F469\u200D\U0001F467", "\U0001F1FA\U0001F1F8", "e\u0301"} {
		if GraphemeCount(value) != 1 {
			t.Errorf("%q counted %d", value, GraphemeCount(value))
		}
	}
	if IsBoundary("e\u0301", 1) {
		t.Fatal("combining mark split")
	}
}
