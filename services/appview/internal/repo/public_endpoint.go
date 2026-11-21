package repo

import (
	"net/url"
	"strconv"
	"strings"
)

// Mirrors PDSResolvedEndpointValidator.validatedBase(_, policy: .publicHTTPS),
// which the snapshot reader uses to bind PLC, did:web and PDS endpoints.

var publicEndpointSpecialUseSuffixes = []string{
	".alt", ".arpa", ".example", ".home.arpa", ".internal", ".invalid",
	".local", ".localdomain", ".localhost", ".onion", ".test",
}

var publicEndpointReservedExampleDomains = []string{"example.com", "example.net", "example.org"}

// validatedPublicBase normalizes an https://host[:port] base URL with a public
// DNS hostname, or returns "" when the endpoint is not acceptable.
func validatedPublicBase(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if strings.Contains(trimmed, "#") {
		return ""
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	if scheme == "" || host == "" || (u.Path != "" && u.Path != "/") {
		return ""
	}
	host = strings.TrimSuffix(host, ".")
	if host == "" || scheme != "https" || !isPublicHostname(host) {
		return ""
	}
	normalized := scheme + "://" + host
	if port := u.Port(); port != "" {
		if _, err := strconv.ParseUint(port, 10, 16); err != nil {
			return ""
		}
		normalized += ":" + port
	} else if strings.HasSuffix(u.Host, ":") {
		return ""
	}
	return normalized
}

func isPublicHostname(host string) bool {
	if isIPLiteralHost(host) || isLoopbackHost(host) || len(host) > 253 || !strings.Contains(host, ".") {
		return false
	}
	for _, suffix := range publicEndpointSpecialUseSuffixes {
		if strings.HasSuffix(host, suffix) {
			return false
		}
	}
	for _, domain := range publicEndpointReservedExampleDomains {
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
			b := label[i]
			if !(isASCIIDigit(b) || isASCIILower(b) || b == '-') {
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

func isIPLiteralHost(host string) bool {
	_, ok := ipv4Octets(host)
	return strings.Contains(host, ":") || ok
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
		for j := 0; j < len(part); j++ {
			if !isASCIIDigit(part[j]) {
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
