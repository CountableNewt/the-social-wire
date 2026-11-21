package atproto

import (
	"context"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"
)

// DNSResolveFunc resolves host to numeric addresses. ok=false means DNS
// failed; an empty slice with ok=true means some answer was non-public.
type DNSResolveFunc func(host string) (addresses []string, ok bool)

// DNSAddressValidator resolves an untrusted HTTPS endpoint immediately before
// use and rejects it unless every returned address is globally routable
// (PublicDNSAddressValidator / RepositoryDNSAddressValidator). Resolutions
// run detached under a bounded admission: a caller that times out or is
// cancelled returns promptly while its resolver keeps the permit until the
// lookup finishes.
type DNSAddressValidator struct {
	permits chan struct{}
	resolve DNSResolveFunc
}

// NewDNSAddressValidator bounds concurrent resolutions to maxConcurrent (min 1).
// A nil resolve uses ResolvePublicAddresses.
func NewDNSAddressValidator(maxConcurrent int, resolve DNSResolveFunc) *DNSAddressValidator {
	if resolve == nil {
		resolve = ResolvePublicAddresses
	}
	return &DNSAddressValidator{permits: make(chan struct{}, max(maxConcurrent, 1)), resolve: resolve}
}

// Process-wide validators, each with its own admission of 8, matching the
// separate Swift shared admissions.
var (
	// PublicDNS guards media, attestation, and other gateway-side public fetches.
	PublicDNS = NewDNSAddressValidator(8, nil)
	// RepositoryDNS guards repository snapshot and scheduling transports.
	RepositoryDNS = NewDNSAddressValidator(8, nil)
)

// DefaultDNSDeadline is the validation deadline when callers pass a zero time.
const DefaultDNSDeadline = 10 * time.Second

// ActiveCount reports resolutions currently holding a permit.
func (v *DNSAddressValidator) ActiveCount() int { return len(v.permits) }

// ValidatedAddress returns the address to pin for baseURL's host, failing
// with ErrPublicTargetInvalid for a non-HTTPS URL or any non-public answer
// and ErrPublicTargetUnavailable for DNS failure, overload, deadline, or
// cancellation. A zero deadline means DefaultDNSDeadline from now.
func (v *DNSAddressValidator) ValidatedAddress(ctx context.Context, baseURL string, deadline time.Time) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || strings.ToLower(parsed.Scheme) != "https" || parsed.Hostname() == "" {
		return "", ErrPublicTargetInvalid
	}
	host := parsed.Hostname()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if deadline.IsZero() {
		deadline = time.Now().Add(DefaultDNSDeadline)
	}
	select {
	case v.permits <- struct{}{}:
	default:
		return "", ErrPublicTargetUnavailable
	}
	type outcome struct {
		addresses []string
		ok        bool
	}
	done := make(chan outcome, 1)
	go func() {
		addresses, ok := v.resolve(host)
		<-v.permits
		done <- outcome{addresses, ok}
	}()
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	var result outcome
	select {
	case result = <-done:
	case <-timer.C:
		return "", ErrPublicTargetUnavailable
	case <-ctx.Done():
		return "", ErrPublicTargetUnavailable
	}
	if !result.ok {
		return "", ErrPublicTargetUnavailable
	}
	address, ok := PreferredPublicAddress(result.addresses)
	if !ok {
		return "", ErrPublicTargetInvalid
	}
	return address, nil
}

// PublicAddress validates rawURL with RepositoryDNS and the default deadline
// (RepositoryDNSAddressValidator.publicAddress).
func PublicAddress(ctx context.Context, rawURL string) (string, error) {
	return RepositoryDNS.ValidatedAddress(ctx, rawURL, time.Time{})
}

// PreferredPublicAddress validates the entire answer set, then prefers IPv4
// (some hosted runtimes lack IPv6 egress). Any unparseable, IPv4-mapped, or
// non-public address rejects the whole set.
func PreferredPublicAddress(addresses []string) (string, bool) {
	if len(addresses) == 0 {
		return "", false
	}
	for _, address := range addresses {
		parsed, err := netip.ParseAddr(address)
		if err != nil || parsed.Zone() != "" {
			return "", false
		}
		if parsed.Is4() {
			bytes := parsed.As4()
			if !IsPublicIPv4(bytes[:]) {
				return "", false
			}
			continue
		}
		bytes := parsed.As16()
		if !IsPublicIPv6(bytes[:]) {
			return "", false
		}
	}
	for _, address := range addresses {
		if !strings.Contains(address, ":") {
			return address, true
		}
	}
	return addresses[0], true
}

// ResolvePublicAddresses looks host up with the system resolver. A non-empty
// result contains only globally routable numeric addresses (sorted, unique);
// if even one answer is special-use the result is empty.
func ResolvePublicAddresses(host string) ([]string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	answers, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(answers) == 0 {
		return nil, false
	}
	unique := map[string]struct{}{}
	for _, answer := range answers {
		if v4 := answer.IP.To4(); v4 != nil {
			if !IsPublicIPv4(v4) {
				return []string{}, true
			}
			unique[net.IP(v4).String()] = struct{}{}
			continue
		}
		v6 := answer.IP.To16()
		if v6 == nil || !IsPublicIPv6(v6) {
			return []string{}, true
		}
		unique[v6.String()] = struct{}{}
	}
	addresses := make([]string, 0, len(unique))
	for address := range unique {
		addresses = append(addresses, address)
	}
	slices.Sort(addresses)
	return addresses, true
}

// IsPublicIPv4 follows the IANA IPv4 special-purpose registry.
func IsPublicIPv4(b []byte) bool {
	if len(b) != 4 {
		return false
	}
	a, b1, c := b[0], b[1], b[2]
	switch {
	case a == 0 || a == 10 || a == 127 || a >= 224:
		return false
	case a == 100 && b1 >= 64 && b1 <= 127:
		return false
	case a == 169 && b1 == 254:
		return false
	case a == 172 && b1 >= 16 && b1 <= 31:
		return false
	case a == 192 && b1 == 168:
		return false
	case a == 192 && b1 == 88 && c == 99:
		return false
	case a == 198 && (b1 == 18 || b1 == 19):
		return false
	case a == 192 && b1 == 0 && (c == 0 || c == 2):
		return false
	case a == 198 && b1 == 51 && c == 100:
		return false
	case a == 203 && b1 == 0 && c == 113:
		return false
	}
	return true
}

// IsPublicIPv6 accepts only ordinary global unicast space in 2000::/3 and
// fails closed on transition, local, documentation, and unassigned prefixes.
func IsPublicIPv6(b []byte) bool {
	if len(b) != 16 {
		return false
	}
	if slices.Equal(b[0:12], []byte{0, 0x64, 0xFF, 0x9B, 0, 0, 0, 0, 0, 0, 0, 0}) {
		// The well-known NAT64 prefix is global, but a translator could turn an
		// embedded private IPv4 address into an SSRF route.
		return IsPublicIPv4(b[12:16])
	}
	if slices.Equal(b[0:6], []byte{0, 0x64, 0xFF, 0x9B, 0, 1}) {
		return false
	}
	if slices.Equal(b[0:8], []byte{1, 0, 0, 0, 0, 0, 0, 1}) {
		return false
	}
	if b[0]&0xE0 != 0x20 {
		return false
	}
	if b[0] == 0x20 && b[1] == 0x01 && b[2]&0xFE == 0 {
		// 2001::/23 is non-global except for its explicitly global entries.
		if b[2] == 0 && b[3] == 1 && allZero(b[4:15]) && b[15] >= 1 && b[15] <= 3 {
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
	if slices.Equal(b[0:4], []byte{0x20, 0x01, 0x0D, 0xB8}) {
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
