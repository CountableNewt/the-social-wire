package atproto

import (
	"net"
	"net/url"
	"strconv"
	"strings"
)

// EndpointPolicy selects which resolved PDS endpoints are acceptable
// (PDSResolvedEndpointPolicy).
type EndpointPolicy int

// Endpoint policies.
const (
	// EndpointPublicHTTPS accepts only public HTTPS hostnames.
	EndpointPublicHTTPS EndpointPolicy = iota
	// EndpointLocalTesting additionally accepts http/https loopback hosts.
	EndpointLocalTesting
)

var specialUseSuffixes = []string{
	".alt", ".arpa", ".example", ".home.arpa", ".internal", ".invalid", ".local",
	".localdomain", ".localhost", ".onion", ".test",
}

var reservedExampleDomains = []string{"example.com", "example.net", "example.org"}

// ValidatedPDSBase normalizes a resolved PDS base URL (lowercase scheme and
// host, no trailing dot, no path) or rejects it: credentials, query,
// fragment, a non-root path, IP literals, special-use names, and non-HTTPS
// schemes all fail (PDSResolvedEndpointValidator.validatedBase).
func ValidatedPDSBase(raw string, policy EndpointPolicy) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if strings.ContainsAny(trimmed, " \t\r\n#") {
		return "", false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Opaque != "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") {
		return "", false
	}
	scheme := strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Hostname())
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return "", false
	}
	port := parsed.Port()
	if port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 0 || n > 65535 {
			return "", false
		}
	}
	loopback := isLoopbackHost(host)
	switch policy {
	case EndpointLocalTesting:
		if loopback {
			if scheme != "http" && scheme != "https" {
				return "", false
			}
		} else if scheme != "https" || !isPublicHostname(host) {
			return "", false
		}
	default:
		if scheme != "https" || !isPublicHostname(host) {
			return "", false
		}
	}
	authority := host
	if strings.Contains(host, ":") {
		authority = "[" + host + "]"
	}
	if port != "" {
		authority = net.JoinHostPort(host, port)
	}
	return scheme + "://" + authority, true
}

func isPublicHostname(host string) bool {
	if isIPLiteral(host) || isLoopbackHost(host) || len(host) > 253 || !strings.Contains(host, ".") {
		return false
	}
	for _, suffix := range specialUseSuffixes {
		if strings.HasSuffix(host, suffix) {
			return false
		}
	}
	for _, domain := range reservedExampleDomains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return false
		}
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'z') && c != '-' {
				return false
			}
		}
	}
	return true
}

func isLoopbackHost(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "::1" {
		return true
	}
	octets, ok := ipv4Octets(host)
	return ok && octets[0] == 127
}

func isIPLiteral(host string) bool {
	if strings.Contains(host, ":") {
		return true
	}
	_, ok := ipv4Octets(host)
	return ok
}

func ipv4Octets(host string) ([4]byte, bool) {
	var octets [4]byte
	parts := strings.Split(host, ".")
	if len(parts) != 4 {
		return octets, false
	}
	for i, part := range parts {
		if part == "" {
			return octets, false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return octets, false
			}
		}
		value, err := strconv.ParseUint(part, 10, 8)
		if err != nil {
			return octets, false
		}
		octets[i] = byte(value)
	}
	return octets, true
}
