package config

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

type databaseConfiguration struct {
	backend, appURL, appToken, schedulingURL, schedulingToken string
}

func loadDatabase(env Env) (databaseConfiguration, error) {
	value := databaseConfiguration{backend: env.StringOr("DATABASE_BACKEND", "sqlite"), appURL: env.String("TURSO_DATABASE_URL"), appToken: env.String("TURSO_AUTH_TOKEN"), schedulingURL: env.String("SCHEDULING_TURSO_DATABASE_URL"), schedulingToken: env.String("SCHEDULING_TURSO_AUTH_TOKEN")}
	if value.backend != "sqlite" && value.backend != "turso" {
		return value, errors.New("DATABASE_BACKEND must be sqlite or turso")
	}
	anyRemote := value.appURL != "" || value.appToken != "" || value.schedulingURL != "" || value.schedulingToken != ""
	if value.backend == "sqlite" {
		if anyRemote {
			return value, errors.New("Turso credentials require DATABASE_BACKEND=turso")
		}
		return value, nil
	}
	if value.appToken == "" || value.schedulingToken == "" || strings.ContainsAny(value.appToken+value.schedulingToken, " \t\r\n") {
		return value, errors.New("Turso requires both server-only authentication tokens")
	}
	app, err := databaseEndpoint(value.appURL)
	if err != nil {
		return value, err
	}
	scheduling, err := databaseEndpoint(value.schedulingURL)
	if err != nil {
		return value, err
	}
	if app == scheduling {
		return value, errors.New("AppView and scheduling require distinct Turso database endpoints")
	}
	value.appURL, value.schedulingURL = app, scheduling
	return value, nil
}

func databaseEndpoint(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "libsql" && parsed.Scheme != "turso" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") || parsed.Opaque != "" {
		return "", errors.New("Turso database URLs must be bare TLS endpoints without credentials, query, path or fragment")
	}
	hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	port := parsed.Port()
	if port == "443" {
		port = ""
	}
	if port != "" {
		parsed.Host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		parsed.Host = "[" + hostname + "]"
	} else {
		parsed.Host = hostname
	}
	parsed.Path = ""
	// libsql://, turso:// and https:// identify the same HTTPS endpoint for this driver.
	parsed.Scheme = "https"
	return parsed.String(), nil
}
