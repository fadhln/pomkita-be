package httpapi_test

import (
	"context"

	appjwt "github.com/fadhln/pomkita-be/internal/jwt"
	"github.com/google/uuid"
)

type SessionView = appjwt.SessionView

type readyStub struct {
	current bool
	pingErr error
}

func (r readyStub) Ping(context.Context) error                           { return r.pingErr }
func (r readyStub) MigrationsCurrent(context.Context, int) (bool, error) { return r.current, nil }

type verifierStub struct{ err error }

func (v verifierStub) Verify(context.Context, string) (appjwt.Claims, error) {
	return appjwt.Claims{Subject: uuid.New(), JTI: uuid.New()}, v.err
}
