// Package edge registers the standalone HTTP edge contract.
package edge

import (
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/CountableNewt/the-social-wire/services/appview/internal/config"
	"github.com/CountableNewt/the-social-wire/services/appview/internal/httpx"
)

const nativePath = "/ios-client-metadata.json"
const webPath = "/oauth-client-metadata.json"

type metadataDocument struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	ClientURI               string   `json:"client_uri"`
	ApplicationType         string   `json:"application_type"`
	RedirectURIs            []string `json:"redirect_uris"`
	Scope                   string   `json:"scope"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	DPoPBoundAccessTokens   bool     `json:"dpop_bound_access_tokens"`
}

// RegisterMetadata publishes the app's web and iOS OAuth metadata.
func RegisterMetadata(router *httpx.Router, core config.Core) {
	for _, path := range []string{webPath, "/oauth/client-metadata.json", nativePath} {
		router.Get(path, func(w http.ResponseWriter, r *http.Request) error {
			scheme, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Proto"), ",")
			scheme = strings.TrimSpace(scheme)
			if scheme == "" {
				scheme = "https"
			}
			origin := scheme + "://" + r.Host
			doc, err := metadata(path, origin, core)
			if err != nil {
				return err
			}
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Del("Access-Control-Allow-Credentials")
			return httpx.JSON(w, http.StatusOK, doc)
		})
	}
}

func metadata(path, origin string, core config.Core) (metadataDocument, error) {
	doc := metadataDocument{
		ClientName: "The Social Wire", ApplicationType: "web",
		GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"},
		TokenEndpointAuthMethod: "none", DPoPBoundAccessTokens: true, Scope: webScopes,
	}
	badOrigin := httpx.Errorf(http.StatusBadRequest, "Invalid metadata origin")
	var redirect string
	switch path {
	case nativePath:
		origin = defaultOrigin(core.OAuthIOSMetadataOrigin, origin)
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Hostname() == "" {
			return doc, badOrigin
		}
		components := strings.Split(parsed.Hostname(), ".")
		slices.Reverse(components)
		redirect = strings.Join(components, ".") + ":/oauth/callback"
		doc.ApplicationType = "native"
		doc.Scope = nativeScopes
	default:
		redirect = defaultOrigin(core.OAuthPublicOrigin, origin) + "/callback"
		path = webPath
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil ||
		(parsed.Scheme != "https" && parsed.Scheme != "http") ||
		parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return doc, badOrigin
	}
	doc.ClientURI = origin
	doc.ClientID = origin + path
	doc.RedirectURIs = []string{redirect}
	return doc, nil
}

func defaultOrigin(configured, fallback string) string {
	if configured == "" {
		return fallback
	}
	return strings.TrimRight(strings.TrimSpace(configured), "/")
}
