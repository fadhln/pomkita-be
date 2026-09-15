package mailer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpool_SendWritesOneJSONMessage(t *testing.T) {
	directory := t.TempDir()
	sender := NewSpool(directory)
	message := Message{To: "user@example.test", Subject: "Activate", TextBody: "https://example.test/aktivasi?token=raw"}

	if err := sender.Send(context.Background(), message); err != nil {
		t.Fatalf("send message: %v", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read spool: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("spool files: got %d, want 1", len(entries))
	}
	contents, err := os.ReadFile(filepath.Join(directory, entries[0].Name()))
	if err != nil {
		t.Fatalf("read spool message: %v", err)
	}
	var got Message
	if err := json.Unmarshal(contents, &got); err != nil {
		t.Fatalf("decode spool message: %v", err)
	}
	if got != message {
		t.Fatalf("message: got %+v, want %+v", got, message)
	}
}

func TestResend_SendUsesConfiguredAPIKeyAndFromAddress(t *testing.T) {
	var authorization, body string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		authorization = request.Header.Get("Authorization")
		contents, _ := io.ReadAll(request.Body)
		body = string(contents)
		writer.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	sender := NewResendWithClient("test-api-key", "no-reply@example.test", server.Client(), server.URL)
	if err := sender.Send(context.Background(), Message{To: "user@example.test", Subject: "Subject", TextBody: "Body"}); err != nil {
		t.Fatalf("send Resend message: %v", err)
	}
	if authorization != "Bearer test-api-key" || !strings.Contains(body, `"from":"no-reply@example.test"`) || !strings.Contains(body, `"text":"Body"`) {
		t.Fatalf("request: authorization=%q body=%s", authorization, body)
	}
}
