package auth

import (
	"context"
	"fmt"
	"strconv"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ogen-go/ogen/ogenerrors"
	"github.com/tgdrive/teldrive/internal/api"
	"github.com/tgdrive/teldrive/internal/cache"
	"github.com/tgdrive/teldrive/internal/config"
	"github.com/tgdrive/teldrive/pkg/models"
	"github.com/tgdrive/teldrive/pkg/types"
	"gorm.io/gorm"
)

type authContextKey string

const authKey authContextKey = "authUser"

func Encode(secret string, claims *types.JWTClaims) (string, error) {

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	return token.SignedString([]byte(secret))
}

func Decode(secret string, token string) (*types.JWTClaims, error) {
	claims := &types.JWTClaims{}

	tkn, err := jwt.ParseWithClaims(token, claims, func(token *jwt.Token) (any, error) {
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	if !tkn.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, err
}

func GetUser(c context.Context) int64 {
	authUser, ok := c.Value(authKey).(*types.JWTClaims)
	if !ok || authUser == nil {
		return 0
	}
	userId, _ := strconv.ParseInt(authUser.Subject, 10, 64)
	return userId
}

func GetJWTUser(c context.Context) *types.JWTClaims {
	authUser, ok := c.Value(authKey).(*types.JWTClaims)
	if !ok {
		return nil
	}
	return authUser
}

func VerifyUser(ctx context.Context, db *gorm.DB, cache cache.Cacher, secret, authCookie string) (*types.JWTClaims, error) {
	claims, err := Decode(secret, authCookie)

	if err != nil {
		return nil, err
	}

	var session *models.Session

	session, err = GetSessionByHash(ctx, db, cache, claims.Hash)

	if err != nil {
		return nil, fmt.Errorf("invalid session")
	}

	claims.TgSession = session.Session

	return claims, nil
}

func GetSessionByHash(ctx context.Context, db *gorm.DB, cache cache.Cacher, hash string) (*models.Session, error) {
	var session models.Session
	key := fmt.Sprintf("sessions:%s", hash)

	err := cache.Get(ctx, key, &session)

	if err != nil {
		if err := db.Model(&models.Session{}).Where("hash = ?", hash).First(&session).Error; err != nil {
			return nil, err
		}
		cache.Set(ctx, key, &session, 0)
	}

	return &session, nil

}

// GetSessionByUserId returns the newest Telegram session belonging to userId.
//
// Share viewers never present the owner's credentials, so anything acting on a
// share's behalf (streaming a file, building a zip) has no session of its own to
// fall back on when the owner has no bots configured. Looking the owner's
// session up by id is what makes those paths work on bot-less deployments.
// The cacher parameter is deliberately not named "cache": that would shadow the
// cache package and put its key helpers out of reach inside this function.
func GetSessionByUserId(ctx context.Context, db *gorm.DB, cacher cache.Cacher, userId int64) (*models.Session, error) {
	var session models.Session
	key := cache.KeySessionUser(userId)

	if err := cacher.Get(ctx, key, &session); err != nil {
		if err := db.Model(&models.Session{}).Where("user_id = ?", userId).
			Order("created_at desc").First(&session).Error; err != nil {
			return nil, err
		}
		cacher.Set(ctx, key, &session, 0)
	}

	return &session, nil
}

type securityHandler struct {
	db    *gorm.DB
	cache cache.Cacher
	cfg   *config.JWTConfig
}

func (s *securityHandler) HandleApiKeyAuth(ctx context.Context, operationName api.OperationName, t api.ApiKeyAuth) (context.Context, error) {
	return s.handleAuth(ctx, t.APIKey)
}

func (s *securityHandler) HandleBearerAuth(ctx context.Context, operationName api.OperationName, t api.BearerAuth) (context.Context, error) {
	return s.handleAuth(ctx, t.Token)
}

func (s *securityHandler) handleAuth(ctx context.Context, token string) (context.Context, error) {
	claims, err := VerifyUser(ctx, s.db, s.cache, s.cfg.Secret, token)
	if err != nil {
		return nil, &ogenerrors.SecurityError{Err: err}
	}
	return context.WithValue(ctx, authKey, claims), nil
}

func NewSecurityHandler(db *gorm.DB, cache cache.Cacher, cfg *config.JWTConfig) api.SecurityHandler {
	return &securityHandler{db: db, cache: cache, cfg: cfg}
}

var _ api.SecurityHandler = (*securityHandler)(nil)
