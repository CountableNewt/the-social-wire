package repo

import (
	"context"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"
)

// DNSResolver returns the numeric addresses for host, nil when resolution
// failed, or an empty slice when any answer is not globally routable.
type DNSResolver func(host string) []string

// sharedDNSAdmission is the process-wide resolver gate.
var sharedDNSAdmission = NewDNSResolverAdmission(8)

const dnsValidationTimeout = 10 * time.Second

// PublicAddress resolves an untrusted HTTPS endpoint immediately before use and
// returns an address to dial, rejecting it unless every DNS answer is globally
// routable. This blocks answers that point at loopback, link-local, private,
// documentation, or multicast networks.
func PublicAddress(ctx context.Context, u *url.URL) (string, error) {
	return ValidatedAddress(ctx, u.String())
}

// ValidatedAddress is PublicAddress for a raw URL string, bounded by 10s or the
// context deadline, whichever is sooner.
func ValidatedAddress(ctx context.Context, baseURL string) (string, error) {
	return validatedAddress(ctx, baseURL, time.Now().Add(dnsValidationTimeout), sharedDNSAdmission, ResolvePublicAddresses)
}

func validatedAddress(ctx context.Context, baseURL string, deadline time.Time, admission *DNSResolverAdmission, resolver DNSResolver) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil || strings.ToLower(u.Scheme) != "https" || u.Hostname() == "" {
		return "", Invalid("DNS rejected")
	}
	host := u.Hostname()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !admission.TryAcquire() {
		return "", Invalid("DNS unavailable")
	}
	type outcome struct{ addresses []string }
	done := make(chan outcome, 1)
	go func() {
		addresses := resolver(host)
		admission.Release()
		done <- outcome{addresses}
	}()
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	var addresses []string
	select {
	case result := <-done:
		addresses = result.addresses
	case <-timer.C:
		return "", Invalid("DNS unavailable")
	case <-ctx.Done():
		return "", Invalid("DNS unavailable")
	}
	if addresses == nil {
		return "", Invalid("DNS unavailable")
	}
	address, ok := PreferredPublicAddress(addresses)
	if !ok {
		return "", Invalid("DNS rejected")
	}
	return address, nil
}

// PreferredPublicAddress validates the entire answer set, then prefers IPv4
// when both families are public: some hosted runtimes have no IPv6 egress.
func PreferredPublicAddress(addresses []string) (string, bool) {
	if len(addresses) == 0 {
		return "", false
	}
	for _, address := range addresses {
		ip, err := netip.ParseAddr(address)
		if err != nil || ip.Zone() != "" {
			return "", false
		}
		if ip.Is4() {
			b := ip.As4()
			if !IsPublicIPv4(b[:]) {
				return "", false
			}
		} else {
			b := ip.As16()
			if !IsPublicIPv6(b[:]) {
				return "", false
			}
		}
	}
	for _, address := range addresses {
		if !strings.Contains(address, ":") {
			return address, true
		}
	}
	return addresses[0], true
}

// ResolveOnlyPublicAddresses: nil means DNS failed; false means no exclusively public answer set.
func ResolveOnlyPublicAddresses(host string) *bool {
	addresses := ResolvePublicAddresses(host)
	if addresses == nil {
		return nil
	}
	result := len(addresses) > 0
	return &result
}

// ResolvePublicAddresses resolves host with the system resolver. A nonempty
// result contains only globally routable numeric addresses (sorted, unique). If
// even one answer is private/special-use the result is empty; nil means failure.
func ResolvePublicAddresses(host string) []string {
	ips, err := net.DefaultResolver.LookupNetIP(context.Background(), "ip", host)
	if err != nil || len(ips) == 0 {
		return nil
	}
	addresses := []string{}
	for _, ip := range ips {
		// The Go resolver may return A answers in IPv4-mapped form.
		ip = ip.Unmap()
		if ip.Is4() {
			b := ip.As4()
			if !IsPublicIPv4(b[:]) {
				return []string{}
			}
		} else {
			b := ip.As16()
			if !IsPublicIPv6(b[:]) {
				return []string{}
			}
		}
		text := ip.String()
		if !slices.Contains(addresses, text) {
			addresses = append(addresses, text)
		}
	}
	slices.Sort(addresses)
	return addresses
}

// IsPublicIPv4 reports whether a 4-byte address is globally routable.
func IsPublicIPv4(b []byte) bool {
	if len(b) != 4 {
		return false
	}
	a, c2, c3 := b[0], b[1], b[2]
	switch {
	case a == 0 || a == 10 || a == 127 || a >= 224:
		return false
	case a == 100 && c2 >= 64 && c2 <= 127:
		return false
	case a == 169 && c2 == 254:
		return false
	case a == 172 && c2 >= 16 && c2 <= 31:
		return false
	case a == 192 && c2 == 168:
		return false
	case a == 192 && c2 == 88 && c3 == 99:
		return false
	case a == 198 && (c2 == 18 || c2 == 19):
		return false
	case a == 192 && c2 == 0 && (c3 == 0 || c3 == 2):
		return false
	case a == 198 && c2 == 51 && c3 == 100:
		return false
	case a == 203 && c2 == 0 && c3 == 113:
		return false
	}
	return true
}

// IsPublicIPv6 reports whether a 16-byte address is globally routable unicast.
func IsPublicIPv6(b []byte) bool {
	if len(b) != 16 {
		return false
	}
	if slices.Equal(b[:12], []byte{0, 0x64, 0xFF, 0x9B, 0, 0, 0, 0, 0, 0, 0, 0}) {
		// The well-known NAT64 prefix is globally reachable, but a translator could
		// otherwise turn an embedded private IPv4 address into an SSRF route.
		return IsPublicIPv4(b[12:])
	}
	if slices.Equal(b[:6], []byte{0, 0x64, 0xFF, 0x9B, 0, 1}) {
		return false
	}
	if slices.Equal(b[:8], []byte{1, 0, 0, 0, 0, 0, 0, 1}) {
		return false
	}
	// Ordinary global unicast is allocated from 2000::/3; everything else fails closed.
	if b[0]&0xE0 != 0x20 {
		return false
	}
	if b[0] == 0x20 && b[1] == 0x01 && b[2]&0xFE == 0 {
		// 2001::/23 is non-global by default; keep only IANA entries marked globally reachable.
		if b[2] == 0 && b[3] == 1 && allZero(b[4:15]) && (b[15] == 1 || b[15] == 2 || b[15] == 3) {
			return true
		}
		if b[2] == 0 && b[3] == 3 {
			return true // AMT 2001:3::/32.
		}
		if slices.Equal(b[2:6], []byte{0, 4, 1, 0x12}) {
			return true // AS112-v6.
		}
		if b[2] == 0 && (b[3]&0xF0 == 0x20 || b[3]&0xF0 == 0x30) {
			return true
		}
		return false
	}
	if slices.Equal(b[:4], []byte{0x20, 0x01, 0x0D, 0xB8}) {
		return false
	}
	if b[0] == 0x20 && b[1] == 0x02 {
		return false // Deprecated 6to4 2002::/16.
	}
	if b[0] == 0x3F && b[1] == 0xFF && b[2]&0xF0 == 0 {
		return false // Documentation 3fff::/20.
	}
	return true
}

func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}
