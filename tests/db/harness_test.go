package db_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tgdrive/teldrive/internal/database"
	jetmodel "github.com/tgdrive/teldrive/internal/database/jet/gen/model"
	"github.com/tgdrive/teldrive/pkg/repositories"
	"github.com/tgdrive/teldrive/tests/testdb"
)

type harness struct {
	t     *testing.T
	ctx   context.Context
	pool  *pgxpool.Pool
	repos *repositories.Repositories
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	ctx := context.Background()
	pool := database.NewTestDatabase(t, true)
	testdb.Reset(t, pool)
	t.Cleanup(func() { pool.Close() })

	return &harness{
		t:     t,
		ctx:   ctx,
		pool:  pool,
		repos: repositories.NewRepositories(pool),
	}
}

func (s *harness) ensureUserExists(userID int64) {
	s.t.Helper()

	_, err := s.repos.Users.GetByID(s.ctx, userID)
	if err == nil {
		return
	}

	now := time.Now().UTC()
	name := "test-user"
	if createErr := s.repos.Users.Create(s.ctx, &jetmodel.Users{
		UserID:    userID,
		UserName:  fmt.Sprintf("user_%d", userID),
		Name:      &name,
		IsPremium: false,
		CreatedAt: now,
		UpdatedAt: now,
	}); createErr != nil {
		s.t.Fatalf("ensure user exists: %v", createErr)
	}
}

func deterministicID(seed string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(seed))
}
