package auth

import "strings"

const bearerPrefix = "Bearer "

// BearerHeader formats a shared secret for the Authorization header.
func BearerHeader(secret string) string {
	return bearerPrefix + secret
}

// ParseBearerToken extracts the token from an Authorization header.
func ParseBearerToken(header string) (string, bool) {
	header = strings.TrimSpace(header)
	if !strings.HasPrefix(header, bearerPrefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, bearerPrefix))
	if token == "" {
		return "", false
	}
	return token, true
}

// BearerTokenMatches reports whether header carries the expected bearer secret.
func BearerTokenMatches(header, secret string) bool {
	got, ok := ParseBearerToken(header)
	if !ok {
		return false
	}
	return got == secret
}
