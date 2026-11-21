package auth

import (
	"context"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/ogen-go/ogen/ogenerrors"
	"github.com/tgdrive/teldrive/internal/admission"
	"github.com/tgdrive/teldrive/internal/cache"
	"github.com/tgdrive/teldrive/pkg/models"
	"github.com/tgdrive/teldrive/pkg/types"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Replace only GORM's SQL execution callback. This exercises real session lookup,
// JWT verification and HTTP error classification without any live DB or Telegram.
func TestStartupAuthHTTPStormRecoversWhenDatabaseReady(t *testing.T) {
	now := time.Unix(100, 0)
	previous := sessionAdmission
	sessionAdmission = admission.New(1, 2, func() time.Time { return now })
	defer func() { sessionAdmission = previous }()
	db, err := gorm.Open(postgres.Open("host=invalid dbname=unused user=unused"), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	var ready atomic.Bool
	var queries atomic.Int32
	err = db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		queries.Add(1)
		if !ready.Load() {
			tx.AddError(&pgconn.PgError{Code: "57P03", Message: "database starting"})
			return
		}
		session := tx.Statement.Dest.(*models.Session)
		*session = models.Session{UserId: 1, Session: "unused"}
		tx.RowsAffected = 1
	})
	if err != nil {
		t.Fatal(err)
	}
	token, err := Encode("secret", &types.JWTClaims{RegisteredClaims: jwt.RegisteredClaims{Subject: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	request := func(value string) int {
		r := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		_, err := VerifyUser(r.Context(), db, cache.NewNoopCache(), "secret", value)
		if err != nil {
			HTTPErrorHandler(r.Context(), w, r, &ogenerrors.SecurityError{Err: err})
		} else {
			w.WriteHeader(204)
		}
		if w.Code == 503 && w.Header().Get("Retry-After") == "" {
			t.Error("503 without Retry-After")
		}
		return w.Code
	}
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if code := request(token); code != 503 {
				t.Errorf("startup status %d", code)
			}
		}()
	}
	wg.Wait()
	if n := queries.Load(); n < 1 || n > 2 {
		t.Fatalf("startup query storm: %d", n)
	}
	now = now.Add(31 * time.Second)
	ready.Store(true)
	if code := request(token); code != 204 {
		t.Fatalf("ready status %d", code)
	}
	count := queries.Load()
	if code := request("invalid"); code != 401 {
		t.Fatalf("invalid status %d", code)
	}
	if queries.Load() != count {
		t.Fatal("invalid JWT queried DB")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sessionAdmission.Begin(ctx, "sessions"); err == nil {
		t.Fatal("cancelled request admitted")
	}
}
