// Package mailer sends application messages through a local spool or Resend.
package mailer

import "context"

// Message is one plain-text email message.
type Message struct {
	To       string `json:"to"`
	Subject  string `json:"subject"`
	TextBody string `json:"text_body"`
}

// Mailer sends one message.
type Mailer interface {
	Send(context.Context, Message) error
}
