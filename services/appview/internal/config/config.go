package config

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// AppEnvironment is the deployment identity; it stays dev/prod even on SQLite.
type AppEnvironment string

// Deployment identities.
const (
	Local AppEnvironment = "local"
	Dev   AppEnvironment = "dev"
	Prod  AppEnvironment = "prod"
)

// Startup errors.
var (
	ErrInvalidEnvironment          = errors.New("APP_ENV must be local, dev, or prod")
	ErrInvalidPort                 = errors.New("PORT must be between 1 and 65535")
	ErrInvalidServiceURL           = errors.New("service URLs must be absolute http(s) URLs without credentials, query, or fragment")
	ErrInvalidBackendMode          = errors.New("BACKEND_MODE must be standalone")
	ErrMissingPersistentSQLitePath = errors.New("SQLITE_DB_PATH must be an absolute path outside local development")
	ErrMissingAttestationSecret    = errors.New("PDS_ATTESTATION_RECEIPT_SECRET is required")
	ErrRetiredIngestionEnabled     = errors.New("retired Wire, Circle, Finance, Sports, and global ingestion must stay disabled")
)

// OAuthGatewayPolicy binds JWT access tokens from registered OAuth clients.
type OAuthGatewayPolicy struct {
	AllowedClientIDs   map[string]struct{}
	AllowedAudiences   map[string]struct{}
	RequireKnownClient bool
}

// Core is the configuration shared by every module (GatewayConfig plus the
// standalone edge settings).
type Core struct {
	AppEnv     AppEnvironment
	PLCURL     string
	ListenHost string
	ListenPort int
	SQLitePath string

	OAuthPublicOrigin            string
	OAuthIOSMetadataOrigin       string
	OAuthOperationsOrigin        string
	OAuthGateway                 OAuthGatewayPolicy
	SupplementalJWKSJSON         string
	GatewayAppViewInternalSecret string
	CORSAllowedOrigins           map[string]struct{}

	// AttestationReceiptSecret keys ATProtoSessionAttestationReceipt.
	AttestationReceiptSecret string

	ProGatingEnabled bool
	ProDIDs          map[string]struct{}

	ToolsEnabled bool
	KlipyAPIKey  string
}

// HasPro reports whether did holds a Pro entitlement.
func (c Core) HasPro(did string) bool {
	_, ok := c.ProDIDs[did]
	return ok
}

// LoadCore validates and returns the shared configuration.
func LoadCore(env Env) (Core, error) {
	appEnv := AppEnvironment(env.StringOr("APP_ENV", string(Local)))
	switch appEnv {
	case Local, Dev, Prod:
	default:
		return Core{}, ErrInvalidEnvironment
	}
	port := 8081
	if raw, ok := env.Optional("PORT"); ok {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 65535 {
			return Core{}, ErrInvalidPort
		}
		port = parsed
	}
	for _, key := range []string{"ATPROTO_PLC_URL", "SEMBLE_PUBLIC_API_BASE_URL"} {
		if raw, ok := env.Optional(key); ok && !validServiceURL(raw) {
			return Core{}, ErrInvalidServiceURL
		}
	}
	if mode := env.StringOr("BACKEND_MODE", "standalone"); mode != "standalone" {
		return Core{}, ErrInvalidBackendMode
	}
	path := env.String("SQLITE_DB_PATH")
	if path == "" && appEnv == Local {
		path = "./social-wire-appview.sqlite"
	}
	if path == "" || (appEnv != Local && !strings.HasPrefix(path, "/")) {
		return Core{}, ErrMissingPersistentSQLitePath
	}
	secret := env.String("PDS_ATTESTATION_RECEIPT_SECRET")
	if secret == "" {
		return Core{}, ErrMissingAttestationSecret
	}
	for _, key := range []string{"WIRE_FEED_MODE", "CIRCLE_FEED_MODE", "FINANCE_FEED_MODE", "SPORTS_FEED_MODE"} {
		if mode := strings.ToLower(env.String(key)); mode == "api" || mode == "visible" {
			return Core{}, ErrRetiredIngestionEnabled
		}
	}
	if env.True("GLOBAL_INGESTION_ENABLED") {
		return Core{}, ErrRetiredIngestionEnabled
	}
	clients := env.Tokens("OAUTH_GATEWAY_ALLOWED_CLIENT_IDS")
	if operations, ok := env.Optional("OAUTH_OPERATIONS_CLIENT_ID"); ok {
		clients[operations] = struct{}{}
	}
	tools := appEnv != Prod
	if raw, ok := env.Optional("TOOLS_ENABLED"); ok {
		tools = strings.ToLower(raw) == "true"
	}
	klipy := env.String("KLIPY_API_KEY")
	if klipy == "" {
		klipy = env.String("KLIPPY_API_KEY")
	}
	return Core{
		AppEnv:                       appEnv,
		PLCURL:                       env.StringOr("ATPROTO_PLC_URL", "https://plc.directory"),
		ListenHost:                   env.StringOr("BIND_HOST", "::"),
		ListenPort:                   port,
		SQLitePath:                   path,
		OAuthPublicOrigin:            env.String("OAUTH_PUBLIC_ORIGIN"),
		OAuthIOSMetadataOrigin:       env.String("OAUTH_IOS_METADATA_ORIGIN"),
		OAuthOperationsOrigin:        env.String("OAUTH_OPERATIONS_ORIGIN"),
		OAuthGateway:                 OAuthGatewayPolicy{AllowedClientIDs: clients, AllowedAudiences: env.Tokens("OAUTH_GATEWAY_ALLOWED_AUDIENCES"), RequireKnownClient: env.Truthy("OAUTH_GATEWAY_REQUIRE_KNOWN_CLIENT")},
		SupplementalJWKSJSON:         env.String("OAUTH_ACCESS_TOKEN_SUPPLEMENTAL_JWKS_JSON"),
		GatewayAppViewInternalSecret: env.String("GATEWAY_APPVIEW_INTERNAL_SECRET"),
		CORSAllowedOrigins:           env.Tokens("CORS_ALLOWED_ORIGINS"),
		AttestationReceiptSecret:     secret,
		ProGatingEnabled:             env.True("SOCIALWIRE_PRO_GATING_ENABLED"),
		ProDIDs:                      env.Tokens("SOCIALWIRE_PRO_DIDS"),
		ToolsEnabled:                 tools,
		KlipyAPIKey:                  klipy,
	}, nil
}

func validServiceURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	return parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
}
