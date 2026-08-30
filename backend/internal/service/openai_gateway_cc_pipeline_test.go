package service

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSetOpenAIUpstreamRequestBodyResetsFraming(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://example.test/v1/chat/completions", strings.NewReader("stale"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Length", "5")
	body := []byte(`{"model":"glm-5.3-flash"}`)

	setOpenAIUpstreamRequestBody(req, body)

	if req.ContentLength != int64(len(body)) {
		t.Fatalf("ContentLength = %d, want %d", req.ContentLength, len(body))
	}
	if got := req.Header.Get("Content-Length"); got != "" {
		t.Fatalf("stale Content-Length header = %q", got)
	}
	if req.TransferEncoding != nil {
		t.Fatalf("TransferEncoding = %v, want nil", req.TransferEncoding)
	}
	got, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("body = %q, want %q", got, body)
	}
	replay, err := req.GetBody()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = replay.Close() }()
	replayed, err := io.ReadAll(replay)
	if err != nil {
		t.Fatal(err)
	}
	if string(replayed) != string(body) {
		t.Fatalf("replayed body = %q, want %q", replayed, body)
	}
}
