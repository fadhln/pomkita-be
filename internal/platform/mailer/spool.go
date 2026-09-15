package mailer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type spool struct {
	directory string
}

// NewSpool creates a mailer that writes one JSON file per message.
func NewSpool(directory string) Mailer {
	return &spool{directory: directory}
}

func (s *spool) Send(ctx context.Context, message Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.directory == "" {
		return errors.New("mailer spool directory is required")
	}
	contents, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode mail message: %w", err)
	}
	if err := os.MkdirAll(s.directory, 0o750); err != nil {
		return fmt.Errorf("create mail spool directory: %w", err)
	}
	nameBytes := make([]byte, 16)
	if _, err := rand.Read(nameBytes); err != nil {
		return fmt.Errorf("create mail spool name: %w", err)
	}
	path := filepath.Join(s.directory, hex.EncodeToString(nameBytes)+".json")
	if err := os.WriteFile(path, contents, 0o640); err != nil {
		return fmt.Errorf("write mail spool message: %w", err)
	}
	return nil
}
