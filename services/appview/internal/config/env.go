// Package config loads process configuration from the environment.
package config

import (
	"bufio"
	"os"
	"strings"
)

// Env is a snapshot of environment variables. Modules read their own keys
// from it so tests can pass a literal map.
type Env map[string]string

// Load merges a dotenv file (DOTENV_PATH, default ./.env) under the process
// environment, as AppEnvironmentLoader.mergeProcessWithDotenv did: process values win.
func Load() Env {
	env := Env{}
	path := os.Getenv("DOTENV_PATH")
	if path == "" {
		path = ".env"
	}
	if file, err := os.Open(path); err == nil {
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			line = strings.TrimPrefix(line, "export ")
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
			env[key] = value
		}
		file.Close()
	}
	for _, entry := range os.Environ() {
		if key, value, ok := strings.Cut(entry, "="); ok {
			env[key] = value
		}
	}
	return env
}

// String returns the trimmed value of key, or "".
func (e Env) String(key string) string { return strings.TrimSpace(e[key]) }

// StringOr returns the trimmed value of key, or fallback when unset or blank.
func (e Env) StringOr(key, fallback string) string {
	if value := e.String(key); value != "" {
		return value
	}
	return fallback
}

// Optional returns the trimmed value and whether it is non-empty.
func (e Env) Optional(key string) (string, bool) {
	value := e.String(key)
	return value, value != ""
}

// True reports whether key is exactly "true", case-insensitively, the check
// Swift used for most feature flags.
func (e Env) True(key string) bool { return strings.ToLower(e.String(key)) == "true" }

// Truthy accepts 1/true/yes/on, OAuthGatewayPolicyParser.truthy.
func (e Env) Truthy(key string) bool {
	switch strings.ToLower(e.String(key)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// Tokens splits key on commas and whitespace into a set, OAuthGatewayPolicyParser.delimiterTokenSet.
func (e Env) Tokens(key string) map[string]struct{} {
	set := map[string]struct{}{}
	for token := range strings.FieldsFuncSeq(e[key], func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	}) {
		set[token] = struct{}{}
	}
	return set
}
