package sqlitedb

import "net/http"

func defaultTursoTransport() http.RoundTripper {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		// A caller-installed global transport may carry custom network/security
		// behavior. Unknown implementations cannot safely be cloned or modified.
		return http.DefaultTransport
	}
	transport := base.Clone()
	// HTTP/1.1 needs one idle TCP connection per simultaneous SQL request.
	// The standard default keeps only two, forcing fresh TLS handshakes when
	// this store's eight retained SQL sessions are used in another burst.
	transport.MaxIdleConnsPerHost = max(transport.MaxIdleConnsPerHost, tursoPoolSize)
	return transport
}
