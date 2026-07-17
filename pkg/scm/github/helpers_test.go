package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const testWebhookSecret = "test-webhook-secret"

func signBody(t *testing.T, body []byte, secret string) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return signaturePrefix + hex.EncodeToString(mac.Sum(nil))
}

func signedRequest(t *testing.T, event string, body []byte) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "/scm/webhook", strings.NewReader(string(body)))
	require.NoError(t, err)
	if event != "" {
		req.Header.Set("X-GitHub-Event", event)
	}
	req.Header.Set("X-Hub-Signature-256", signBody(t, body, testWebhookSecret))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func issueCommentPayload(t *testing.T, action string, onPR bool, commentBody string) []byte {
	t.Helper()
	issue := map[string]any{"number": 42}
	if onPR {
		issue["pull_request"] = map[string]any{}
	}
	payload := map[string]any{
		"action": action,
		"issue":  issue,
		"comment": map[string]any{
			"id":   1,
			"body": commentBody,
			"user": map[string]any{"login": "jace"},
		},
		"repository": map[string]any{
			"full_name": "acme/infra",
		},
	}
	b, err := json.Marshal(payload)
	require.NoError(t, err)
	return b
}
