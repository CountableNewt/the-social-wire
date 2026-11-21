package repo

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"
)

func TestPublicIPClassification(t *testing.T) {
	for address, want := range map[string]bool{
		"8.8.8.8": true, "1.1.1.1": true, "10.1.2.3": false, "127.0.0.1": false,
		"100.64.0.1": false, "169.254.1.1": false, "172.16.0.1": false, "192.168.1.1": false,
		"192.0.2.1": false, "198.51.100.1": false, "203.0.113.1": false, "224.0.0.1": false,
		"2606:4700:4700::1111": true, "::1": false, "fe80::1": false, "fc00::1": false,
		"2001:db8::1": false, "2002::1": false, "64:ff9b::808:808": true, "64:ff9b::a00:1": false,
		"2001:1::1": true, "2001:3::1": true, "2001:20::1": true, "2001::1": false, "3fff::1": false,
	} {
		ip := netip.MustParseAddr(address)
		var got bool
		if ip.Is4() {
			b := ip.As4()
			got = IsPublicIPv4(b[:])
		} else {
			b := ip.As16()
			got = IsPublicIPv6(b[:])
		}
		if got != want {
			t.Errorf("%s public = %v, want %v", address, got, want)
		}
	}
}

func TestValidatedAddressUsesWholeAnswerSet(t *testing.T) {
	ctx := context.Background()
	deadline := time.Now().Add(time.Second)
	admission := NewDNSResolverAdmission(8)
	resolve := func(answers []string) DNSResolver { return func(string) []string { return answers } }
	if got, err := validatedAddress(ctx, "https://pds.example.social/x", deadline, admission, resolve([]string{"2606:4700:4700::1111", "8.8.8.8"})); err != nil || got != "8.8.8.8" {
		t.Fatalf("got %q err %v", got, err)
	}
	if _, err := validatedAddress(ctx, "https://pds.example.social", deadline, admission, resolve([]string{})); !errors.Is(err, Invalid("DNS rejected")) {
		t.Fatalf("err = %v", err)
	}
	if _, err := validatedAddress(ctx, "https://pds.example.social", deadline, admission, resolve(nil)); !errors.Is(err, Invalid("DNS unavailable")) {
		t.Fatalf("err = %v", err)
	}
	if _, err := validatedAddress(ctx, "http://pds.example.social", deadline, admission, resolve([]string{"8.8.8.8"})); !errors.Is(err, Invalid("DNS rejected")) {
		t.Fatalf("err = %v", err)
	}
	if admission.ActiveCount() != 0 {
		t.Fatalf("active = %d", admission.ActiveCount())
	}
}

func TestValidatedAddressTimesOutAndHoldsAdmissionUntilResolverReturns(t *testing.T) {
	admission := NewDNSResolverAdmission(1)
	release := make(chan struct{})
	slow := func(string) []string { <-release; return []string{"8.8.8.8"} }
	_, err := validatedAddress(context.Background(), "https://pds.example.social", time.Now().Add(5*time.Millisecond), admission, slow)
	if !errors.Is(err, Invalid("DNS unavailable")) {
		t.Fatalf("err = %v", err)
	}
	if _, err := validatedAddress(context.Background(), "https://pds.example.social", time.Now().Add(time.Second), admission, slow); !errors.Is(err, Invalid("DNS unavailable")) {
		t.Fatalf("admission not enforced: %v", err)
	}
	close(release)
	for admission.ActiveCount() != 0 {
		time.Sleep(time.Millisecond)
	}
}

func TestValidatedPublicBase(t *testing.T) {
	for raw, want := range map[string]string{
		"https://PDS.Publisher.Social/":     "https://pds.publisher.social",
		" https://pds.publisher.social ":    "https://pds.publisher.social",
		"https://pds.publisher.social.":     "https://pds.publisher.social",
		"https://pds.publisher.social:8443": "https://pds.publisher.social:8443",
		"http://pds.publisher.social":       "",
		"https://localhost":                 "",
		"https://127.0.0.1":                 "",
		"https://pds.local":                 "",
		"https://foo.example.com":           "",
		"https://pds.publisher.social/x":    "",
		"https://pds.publisher.social?q":    "",
		"https://pds.publisher.social#f":    "",
		"https://user@pds.publisher.social": "",
		"https://singlelabel":               "",
	} {
		if got := validatedPublicBase(raw); got != want {
			t.Errorf("validatedPublicBase(%q) = %q, want %q", raw, got, want)
		}
	}
}
