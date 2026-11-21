package isotime

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseKeepsFractionAndOffset(t *testing.T) {
	got, err := Parse("2026-11-20T12:00:00.123456+02:00")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 11, 20, 10, 0, 0, 123456000, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestParseRejectsLooseShapes(t *testing.T) {
	for _, value := range []string{"2026-11-20", "2026-11-20T12:00:00", "2026-11-20 12:00:00Z", "2026-11-20T12:00:00.Z"} {
		if _, err := Parse(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
}

func TestFormats(t *testing.T) {
	at := time.Date(2026, 11, 20, 12, 0, 0, 5000, time.FixedZone("x", 3600))
	if got := Format(at); got != "2026-11-20T11:00:00Z" {
		t.Fatal(got)
	}
	if got := FormatMicros(at); got != "2026-11-20T11:00:00.000005Z" {
		t.Fatal(got)
	}
}

func TestTimeJSONRoundTrip(t *testing.T) {
	data, err := json.Marshal(struct {
		At Time `json:"at"`
	}{Time{time.Date(2026, 1, 2, 3, 4, 5, 999, time.UTC)}})
	if err != nil || string(data) != `{"at":"2026-01-02T03:04:05Z"}` {
		t.Fatalf("%s %v", data, err)
	}
	var decoded Time
	if err := json.Unmarshal([]byte(`"2026-01-02T03:04:05.5Z"`), &decoded); err != nil || decoded.Nanosecond() != 500000000 {
		t.Fatalf("%v %v", decoded, err)
	}
}
