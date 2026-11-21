package operations

import "testing"

func TestTraceparentRoundTrips(t *testing.T) {
	context := TraceContext{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7", Sampled: true}
	parsed, ok := ParseTraceparent(context.Traceparent())
	if !ok || parsed.TraceID != context.TraceID || parsed.SpanID != context.SpanID || !parsed.Sampled {
		t.Fatalf("parsed %+v ok=%v", parsed, ok)
	}
	if child := parsed.Child(); child.TraceID != context.TraceID || child.SpanID == context.SpanID {
		t.Fatalf("child %+v", child)
	}
	if got := context.Traceparent(); got != "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" {
		t.Fatalf("traceparent %q", got)
	}
}

func TestTraceparentRejectsInvalid(t *testing.T) {
	for _, value := range []string{
		"00-00000000000000000000000000000000-0000000000000000-01",
		"01-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"00-4bf92f3577b34da6a3ce929d0e0e473-00f067aa0ba902b7-01",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-zz",
		"garbage",
	} {
		if _, ok := ParseTraceparent(value); ok {
			t.Errorf("accepted %q", value)
		}
	}
	if parsed, ok := ParseTraceparent("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00"); !ok || parsed.Sampled {
		t.Fatalf("unsampled flag: %+v %v", parsed, ok)
	}
}
