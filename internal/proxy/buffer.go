package proxy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
)

var ErrRequestBodyTooLarge = errors.New("request body exceeds maximum allowed size")

type BufferedRequest struct {
	Body []byte
}

func NewBufferedRequestWithLimit(r *http.Request, maxBytes int64) (*BufferedRequest, error) {
	if r.Body == nil || r.Body == http.NoBody {
		return &BufferedRequest{Body: nil}, nil
	}

	lr := io.LimitReader(r.Body, maxBytes+1)
	body, err := io.ReadAll(lr)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}

	if int64(len(body)) > maxBytes {
		return nil, ErrRequestBodyTooLarge
	}

	// Restore original body
	r.Body = io.NopCloser(bytes.NewReader(body))
	return &BufferedRequest{Body: body}, nil
}

func (b *BufferedRequest) ResetBody(r *http.Request) {
	if len(b.Body) == 0 {
		r.Body = http.NoBody
		r.ContentLength = 0
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(b.Body))
	r.ContentLength = int64(len(b.Body))
}
