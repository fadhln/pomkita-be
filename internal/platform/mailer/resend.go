package mailer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

type resend struct {
	apiKey string
	from   string
	client *http.Client
	url    string
}

// NewResend creates a mailer that sends messages through the Resend API.
func NewResend(apiKey, from string) Mailer {
	return &resend{apiKey: apiKey, from: from, client: http.DefaultClient, url: "https://api.resend.com/emails"}
}

// NewResendWithClient creates a Resend mailer with an injected HTTP client.
func NewResendWithClient(apiKey, from string, client *http.Client, endpoint string) Mailer {
	if client == nil {
		client = http.DefaultClient
	}
	if endpoint == "" {
		endpoint = "https://api.resend.com/emails"
	}
	return &resend{apiKey: apiKey, from: from, client: client, url: endpoint}
}

func (r *resend) Send(ctx context.Context, message Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || r.apiKey == "" || r.from == "" {
		return errors.New("Resend mailer configuration is incomplete")
	}
	payload, err := json.Marshal(struct {
		From    string `json:"from"`
		To      string `json:"to"`
		Subject string `json:"subject"`
		Text    string `json:"text"`
	}{From: r.from, To: message.To, Subject: message.Subject, Text: message.TextBody})
	if err != nil {
		return fmt.Errorf("encode Resend message: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create Resend request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+r.apiKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := r.client.Do(request)
	if err != nil {
		return fmt.Errorf("send Resend message: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Resend API returned status %d", response.StatusCode)
	}
	return nil
}
