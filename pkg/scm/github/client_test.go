package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetFile(t *testing.T) {
	content := base64.StdEncoding.EncodeToString([]byte("stacks:\n  - name: test\n"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/contents/terraplane.yaml" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("ref"); got != "abc123" {
			t.Fatalf("ref = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"encoding": "base64",
			"content":  content,
		})
	}))
	defer srv.Close()

	c := &client{
		accessToken: "token",
		httpClient:  srv.Client(),
		apiURL:      srv.URL,
	}

	got, err := c.GetFile(context.Background(), "o/r", "terraplane.yaml", "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if got != "stacks:\n  - name: test\n" {
		t.Fatalf("content = %q", got)
	}
}

func TestGetPullRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/pulls/1" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"head": map[string]string{
				"ref": "feature",
				"sha": "deadbeef",
			},
		})
	}))
	defer srv.Close()

	c := &client{
		accessToken: "token",
		httpClient:  srv.Client(),
		apiURL:      srv.URL,
	}

	pr, err := c.GetPullRequest(context.Background(), "o/r", 1)
	if err != nil {
		t.Fatal(err)
	}
	if pr.HeadSHA != "deadbeef" || pr.HeadRef != "feature" {
		t.Fatalf("pr = %+v", pr)
	}
}
