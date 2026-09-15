package httpapi_test

import (
	"context"

	"github.com/google/uuid"
	appjwt "github.com/pomkita/pomkita-be/internal/jwt"
)

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
