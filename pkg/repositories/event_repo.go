package repositories

import (
	"context"
	"errors"
	"time"

	"github.com/go-jet/jet/v2/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tgdrive/teldrive/internal/database/jet/gen/model"
	"github.com/tgdrive/teldrive/internal/database/jet/gen/table"
)

type JetEventRepository struct {
	db jetDB
}

func NewJetEventRepository(pool *pgxpool.Pool) *JetEventRepository {
	return &JetEventRepository{db: newJetDB(pool)}
}

func (r *JetEventRepository) Create(ctx context.Context, event *model.Events) error {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}

	_, err := r.db.executor(ctx).Exec(ctx, `
		INSERT INTO events (id, type, user_id, source, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, event.ID, event.Type, event.UserID, event.Source, event.CreatedAt)
	return normalizeDBError(err)
}

func (r *JetEventRepository) CreateReturningSeq(ctx context.Context, event *model.Events) (int64, error) {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}

	var seq int64
	err := r.db.executor(ctx).QueryRow(ctx, `
		INSERT INTO events (id, type, user_id, source, created_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING seq
	`, event.ID, event.Type, event.UserID, event.Source, event.CreatedAt).Scan(&seq)
	if err != nil {
		return 0, normalizeDBError(err)
	}

	return seq, nil
}

func (r *JetEventRepository) GetByUserID(ctx context.Context, userID int64, since time.Time) ([]model.Events, error) {
	stmt := table.Events.
		SELECT(table.Events.AllColumns).
		FROM(table.Events).
		WHERE(
			table.Events.UserID.EQ(postgres.Int64(userID)).
				AND(table.Events.CreatedAt.GT(postgres.TimestampT(since))),
		).
		ORDER_BY(table.Events.CreatedAt.DESC())

	var out []model.Events
	if err := r.db.query(ctx, stmt, &out); err != nil {
		return nil, err
	}

	return out, nil
}

func (r *JetEventRepository) GetRecent(ctx context.Context, userID int64, since time.Time, limit int) ([]model.Events, error) {
	stmt := table.Events.
		SELECT(table.Events.AllColumns).
		FROM(table.Events).
		WHERE(
			table.Events.UserID.EQ(postgres.Int64(userID)).
				AND(table.Events.CreatedAt.GT(postgres.TimestampT(since))),
		).
		ORDER_BY(table.Events.CreatedAt.DESC())

	if limit > 0 {
		stmt = stmt.LIMIT(int64(limit))
	}

	var out []model.Events
	if err := r.db.query(ctx, stmt, &out); err != nil {
		return nil, err
	}

	return out, nil
}

func (r *JetEventRepository) GetSince(ctx context.Context, since time.Time, limit int) ([]model.Events, error) {
	stmt := table.Events.
		SELECT(table.Events.AllColumns).
		FROM(table.Events).
		WHERE(table.Events.CreatedAt.GT(postgres.TimestampT(since))).
		ORDER_BY(table.Events.CreatedAt.ASC())

	if limit > 0 {
		stmt = stmt.LIMIT(int64(limit))
	}

	var out []model.Events
	if err := r.db.query(ctx, stmt, &out); err != nil {
		return nil, err
	}

	return out, nil
}

func (r *JetEventRepository) GetBySeq(ctx context.Context, seq int64) (*EventStreamItem, error) {
	row := r.db.executor(ctx).QueryRow(ctx, `
		SELECT seq, id, type, user_id, source, created_at
		FROM events
		WHERE seq = $1
	`, seq)

	item, err := scanEventStreamItem(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return item, nil
}

func (r *JetEventRepository) GetAfterSeq(ctx context.Context, seq int64, limit int) ([]EventStreamItem, error) {
	if limit <= 0 {
		limit = 1000
	}

	rows, err := r.db.executor(ctx).Query(ctx, `
		SELECT seq, id, type, user_id, source, created_at
		FROM events
		WHERE seq > $1
		ORDER BY seq ASC
		LIMIT $2
	`, seq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEventStreamItems(rows)
}

func (r *JetEventRepository) GetAfterSeqForUser(ctx context.Context, userID int64, seq int64, eventTypes []string, limit int) ([]EventStreamItem, error) {
	if limit <= 0 {
		limit = 1000
	}

	args := []any{userID, seq, limit}
	query := `
		SELECT seq, id, type, user_id, source, created_at
		FROM events
		WHERE user_id = $1 AND seq > $2
	`
	if len(eventTypes) > 0 {
		query += " AND type = ANY($4)"
		args = append(args, eventTypes)
	}
	query += " ORDER BY seq ASC LIMIT $3"

	rows, err := r.db.executor(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEventStreamItems(rows)
}

func (r *JetEventRepository) MaxSeq(ctx context.Context) (int64, error) {
	var seq int64
	err := r.db.executor(ctx).QueryRow(ctx, "SELECT COALESCE(MAX(seq), 0) FROM events").Scan(&seq)
	return seq, err
}

func (r *JetEventRepository) DeleteOlderThan(ctx context.Context, before time.Time) (int64, error) {
	stmt := table.Events.DELETE().WHERE(table.Events.CreatedAt.LT(postgres.TimestampT(before)))

	tag, err := r.db.execTag(ctx, stmt)
	if err != nil {
		return 0, err
	}

	return tag.RowsAffected(), nil
}

func (r *JetEventRepository) DeleteOlderThanForUser(ctx context.Context, userID int64, before time.Time) (int64, error) {
	stmt := table.Events.DELETE().WHERE(
		table.Events.UserID.EQ(postgres.Int64(userID)).
			AND(table.Events.CreatedAt.LT(postgres.TimestampT(before))),
	)

	tag, err := r.db.execTag(ctx, stmt)
	if err != nil {
		return 0, err
	}

	return tag.RowsAffected(), nil
}

type eventStreamScanner interface {
	Scan(dest ...any) error
}

func scanEventStreamItem(row eventStreamScanner) (*EventStreamItem, error) {
	var item EventStreamItem
	if err := row.Scan(&item.Seq, &item.ID, &item.Type, &item.UserID, &item.Source, &item.CreatedAt); err != nil {
		return nil, err
	}
	return &item, nil
}

func scanEventStreamItems(rows pgx.Rows) ([]EventStreamItem, error) {
	out := make([]EventStreamItem, 0)
	for rows.Next() {
		item, err := scanEventStreamItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}
