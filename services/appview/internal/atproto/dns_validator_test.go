package atproto

import (
	"context"
	"errors"
	"testing"
	"time"
)

func ipv6Bytes(groups []uint16) []byte {
	out := make([]byte, 0, 16)
	for _, g := range groups {
		out = append(out, byte(g>>8), byte(g))
	}
	return out
}

func TestPublicDNSAddressClassification(t *testing.T) {
	ipv4Cases := []struct {
		address  []byte
		expected bool
	}{
		{[]byte{0, 0, 0, 1}, false}, {[]byte{10, 0, 0, 1}, false}, {[]byte{100, 64, 0, 1}, false},
		{[]byte{127, 0, 0, 1}, false}, {[]byte{169, 254, 1, 1}, false}, {[]byte{172, 16, 0, 1}, false},
		{[]byte{192, 0, 0, 1}, false}, {[]byte{192, 0, 2, 1}, false}, {[]byte{192, 88, 99, 1}, false},
		{[]byte{192, 168, 1, 1}, false}, {[]byte{198, 18, 0, 1}, false}, {[]byte{198, 51, 100, 1}, false},
		{[]byte{203, 0, 113, 1}, false}, {[]byte{224, 0, 0, 1}, false}, {[]byte{240, 0, 0, 1}, false},
		{[]byte{1, 1, 1, 1}, true}, {[]byte{8, 8, 8, 8}, true},
	}
	for _, c := range ipv4Cases {
		if IsPublicIPv4(c.address) != c.expected {
			t.Errorf("IPv4 %v expected %v", c.address, c.expected)
		}
	}
	ipv6Cases := []struct {
		groups   []uint16
		expected bool
	}{
		{[]uint16{0, 0, 0, 0, 0, 0, 0, 0}, false},
		{[]uint16{0, 0, 0, 0, 0, 0, 0, 1}, false},
		{[]uint16{0x64, 0xFF9B, 0, 0, 0, 0, 0x0808, 0x0808}, true},
		{[]uint16{0x64, 0xFF9B, 0, 0, 0, 0, 0x0A00, 1}, false},
		{[]uint16{0x64, 0xFF9B, 1, 0, 0, 0, 0, 1}, false},
		{[]uint16{0x100, 0, 0, 0, 0, 0, 0, 1}, false},
		{[]uint16{0x100, 0, 0, 1, 0, 0, 0, 1}, false},
		{[]uint16{0x2001, 0, 0, 0, 0, 0, 0, 1}, false},
		{[]uint16{0x2001, 1, 0, 0, 0, 0, 0, 1}, true},
		{[]uint16{0x2001, 0x0002, 0, 0, 0, 0, 0, 1}, false},
		{[]uint16{0x2001, 0x0003, 0, 0, 0, 0, 0, 1}, true},
		{[]uint16{0x2001, 0x0004, 0x0112, 0, 0, 0, 0, 1}, true},
		{[]uint16{0x2001, 0x0005, 0, 0, 0, 0, 0, 1}, false},
		{[]uint16{0x2001, 0x0010, 0, 0, 0, 0, 0, 1}, false},
		{[]uint16{0x2001, 0x0020, 0, 0, 0, 0, 0, 1}, true},
		{[]uint16{0x2001, 0x0030, 0, 0, 0, 0, 0, 1}, true},
		{[]uint16{0x2001, 0x0DB8, 0, 0, 0, 0, 0, 1}, false},
		{[]uint16{0x2002, 0, 0, 0, 0, 0, 0, 1}, false},
		{[]uint16{0x3FFF, 0, 0, 0, 0, 0, 0, 1}, false},
		{[]uint16{0x5F00, 0, 0, 0, 0, 0, 0, 1}, false},
		{[]uint16{0xFC00, 0, 0, 0, 0, 0, 0, 1}, false},
		{[]uint16{0xFE80, 0, 0, 0, 0, 0, 0, 1}, false},
		{[]uint16{0xFF00, 0, 0, 0, 0, 0, 0, 1}, false},
		{[]uint16{0x2001, 0x4860, 0x4860, 0, 0, 0, 0, 0x8888}, true},
		{[]uint16{0x2606, 0x4700, 0x4700, 0, 0, 0, 0, 0x1111}, true},
	}
	for _, c := range ipv6Cases {
		if IsPublicIPv6(ipv6Bytes(c.groups)) != c.expected {
			t.Errorf("IPv6 %x expected %v", c.groups, c.expected)
		}
	}
}

func fixedResolver(addresses ...string) DNSResolveFunc {
	return func(string) ([]string, bool) { return addresses, true }
}

func TestDNSPinningPrefersPublicIPv4(t *testing.T) {
	ipv6 := "2600:1f10:4c2d:4711:9d8a:76f6:4afb:80cd"
	ipv4 := "3.228.126.156"
	ctx := context.Background()
	got, err := NewDNSAddressValidator(8, fixedResolver(ipv6, ipv4)).ValidatedAddress(ctx, "https://tracker.example", time.Time{})
	if err != nil || got != ipv4 {
		t.Fatalf("dual stack %q %v", got, err)
	}
	got, err = NewDNSAddressValidator(8, fixedResolver(ipv6)).ValidatedAddress(ctx, "https://ipv6.example", time.Time{})
	if err != nil || got != ipv6 {
		t.Fatalf("ipv6 only %q %v", got, err)
	}
}

func TestDNSRejectsMixedUnsafeAnswers(t *testing.T) {
	for _, set := range [][]string{
		{"3.228.126.156", "127.0.0.1"},
		{"2600:1f10:4c2d:4711:9d8a:76f6:4afb:80cd", "10.0.0.1"},
		{"3.228.126.156", "::1"},
		{"3.228.126.156", "::ffff:127.0.0.1"},
		{"3.228.126.156", "not-an-address"},
	} {
		_, err := NewDNSAddressValidator(8, fixedResolver(set...)).ValidatedAddress(context.Background(), "https://tracker.example", time.Time{})
		if !errors.Is(err, ErrPublicTargetInvalid) {
			t.Errorf("%v: expected invalid, got %v", set, err)
		}
	}
	if got, ok := PreferredPublicAddress([]string{"2606:4700:4700::1111", "8.8.8.8"}); !ok || got != "8.8.8.8" {
		t.Fatalf("preferred %q", got)
	}
}

func TestDNSRejectsNonHTTPSAndFailedResolution(t *testing.T) {
	v := NewDNSAddressValidator(8, func(string) ([]string, bool) { return nil, false })
	if _, err := v.ValidatedAddress(context.Background(), "http://example.com", time.Time{}); !errors.Is(err, ErrPublicTargetInvalid) {
		t.Fatalf("http accepted: %v", err)
	}
	if _, err := v.ValidatedAddress(context.Background(), "https://example.com", time.Time{}); !errors.Is(err, ErrPublicTargetUnavailable) {
		t.Fatalf("failed DNS: %v", err)
	}
}

func waitForActive(t *testing.T, v *DNSAddressValidator, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for v.ActiveCount() != want && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if v.ActiveCount() != want {
		t.Fatalf("active count %d, want %d", v.ActiveCount(), want)
	}
}

func TestDNSDeadlineKeepsBlockedPermit(t *testing.T) {
	release := make(chan struct{})
	blocking := func(host string) ([]string, bool) {
		if host == "slow.public.social" {
			<-release
			return []string{"1.1.1.1"}, true
		}
		return []string{"8.8.8.8"}, true
	}
	v := NewDNSAddressValidator(1, blocking)
	ctx := context.Background()
	if _, err := v.ValidatedAddress(ctx, "https://slow.public.social", time.Now().Add(20*time.Millisecond)); !errors.Is(err, ErrPublicTargetUnavailable) {
		t.Fatalf("deadline: %v", err)
	}
	if _, err := v.ValidatedAddress(ctx, "https://other.public.social", time.Now().Add(time.Second)); !errors.Is(err, ErrPublicTargetUnavailable) {
		t.Fatalf("admission should be exhausted: %v", err)
	}
	close(release)
	waitForActive(t, v, 0)
	got, err := v.ValidatedAddress(ctx, "https://recovered.public.social", time.Now().Add(time.Second))
	if err != nil || got != "8.8.8.8" {
		t.Fatalf("recovered %q %v", got, err)
	}
}

func TestDNSCancellationReturnsPromptly(t *testing.T) {
	release := make(chan struct{})
	v := NewDNSAddressValidator(1, func(string) ([]string, bool) {
		<-release
		return []string{"1.1.1.1"}, true
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := v.ValidatedAddress(ctx, "https://cancelled.public.social", time.Now().Add(time.Second))
		result <- err
	}()
	waitForActive(t, v, 1)
	cancel()
	if err := <-result; !errors.Is(err, ErrPublicTargetUnavailable) {
		t.Fatalf("cancelled: %v", err)
	}
	if v.ActiveCount() != 1 {
		t.Fatal("blocked resolver must keep its permit")
	}
	close(release)
	waitForActive(t, v, 0)
}
