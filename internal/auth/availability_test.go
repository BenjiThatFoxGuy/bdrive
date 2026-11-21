package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/tgdrive/teldrive/pkg/models"
	"github.com/tgdrive/teldrive/pkg/types"
	"gorm.io/gorm"
)

func TestVerifyUserDistinguishesUnavailableFromInvalid(t *testing.T) {
	token, err := Encode("secret", &types.JWTClaims{RegisteredClaims: jwt.RegisteredClaims{Subject: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	unavailable := errors.New("database starting")
	for _, tt := range []struct {
		name            string
		lookupErr       error
		wantUnavailable bool
	}{
		{"startup unavailable", unavailable, true}, {"unknown session", gorm.ErrRecordNotFound, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := verifyUser(context.Background(), "secret", token, func(context.Context, string) (*models.Session, error) { return nil, tt.lookupErr })
			if err == nil || errors.Is(err, ErrUnavailable) != tt.wantUnavailable {
				t.Fatalf("wrong auth classification: %v", err)
			}
		})
	}
	called := false
	_, err = verifyUser(context.Background(), "secret", "invalid", func(context.Context, string) (*models.Session, error) { called = true; return nil, unavailable })
	if err == nil || called || errors.Is(err, ErrUnavailable) {
		t.Fatalf("invalid token touched dependency: %v %v", called, err)
	}
}
