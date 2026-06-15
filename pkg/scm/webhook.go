package scm

import (
	"bytes"
	"io"
	"net/http"
)

type Webhook struct {
	Headers http.Header
	Body    []byte
}

func WebhookFromRequest(r *http.Request) (Webhook, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return Webhook{}, err
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	return Webhook{
		Headers: r.Header.Clone(),
		Body:    body,
	}, nil
}
