package operations

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"slices"
	"strings"
	"unicode"
)

var prohibitedAttributeKeys = []string{
	"authorization", "dpop", "cookie", "token", "secret", "password", "record", "body",
}

// BoundedAttributes is OperationsRedactor.boundedAttributes: at most 32 keys
// (taken in sorted order), credential- and body-like keys removed, values
// truncated to 256 characters.
func BoundedAttributes(input map[string]string) map[string]string {
	return boundedAttributes(input, 256)
}

func boundedAttributes(input map[string]string, maximumValueLength int) map[string]string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if len(keys) > 32 {
		keys = keys[:32]
	}
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		lower := strings.ToLower(key)
		if slices.ContainsFunc(prohibitedAttributeKeys, func(p string) bool { return strings.Contains(lower, p) }) {
			continue
		}
		result[key] = prefixRunes(input[key], maximumValueLength)
	}
	return result
}

// RecordIdentifierHash is the first 12 bytes of SHA-256, as 24 hex characters.
func RecordIdentifierHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:12])
}

// HashIdentity hashes an identity the same way as RecordIdentifierHash.
func HashIdentity(value string) string { return RecordIdentifierHash(value) }

// ErrorCategory is OperationsRedactor.errorCategory for non-Postgres errors: the
// error's unqualified type name, non-alphanumerics replaced by "_", at most 64
// characters, lowercased. It never includes the error message.
func ErrorCategory(err error) string {
	if err == nil {
		return "unknown"
	}
	name := reflect.TypeOf(err).String()
	if index := strings.LastIndex(name, "."); index >= 0 {
		name = name[index+1:]
	}
	name = strings.TrimLeft(name, "*")
	var builder strings.Builder
	for _, r := range name {
		if isAlphanumeric(r) {
			builder.WriteRune(r)
		} else {
			builder.WriteRune('_')
		}
	}
	return strings.ToLower(prefixRunes(builder.String(), 64))
}

// isAlphanumeric matches Foundation's CharacterSet.alphanumerics (letters, marks, numbers).
func isAlphanumeric(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsNumber(r)
}

func prefixRunes(value string, limit int) string {
	count := 0
	for index := range value {
		if count == limit {
			return value[:index]
		}
		count++
	}
	return value
}
