package db_test

import (
	"context"
	"errors"
	"testing"

	jetmodel "github.com/tgdrive/teldrive/internal/database/jet/gen/model"
	"github.com/tgdrive/teldrive/pkg/repositories"
)

func TestKVSetAndGet(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()

	kvRepo := repositories.NewJetKVRepository(s.pool)

	key := "test-kv-setget-key"
	value := []byte("hello world")

	if err := kvRepo.Set(ctx, &jetmodel.Kv{Key: key, Value: value}); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	got, err := kvRepo.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(got.Value) != "hello world" {
		t.Fatalf("value mismatch: got %s, want hello world", string(got.Value))
	}
}

func TestKVGet_NotFound(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()

	kvRepo := repositories.NewJetKVRepository(s.pool)

	_, err := kvRepo.Get(ctx, "test-kv-notfound-nonexistent")
	if !errors.Is(err, repositories.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestKVDelete(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()

	kvRepo := repositories.NewJetKVRepository(s.pool)

	key := "test-kv-delete-key"
	value := []byte("delete me")

	if err := kvRepo.Set(ctx, &jetmodel.Kv{Key: key, Value: value}); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	if err := kvRepo.Delete(ctx, key); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err := kvRepo.Get(ctx, key)
	if !errors.Is(err, repositories.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestKVDeletePrefix(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()

	kvRepo := repositories.NewJetKVRepository(s.pool)

	prefix := "test-kv-prefix-"
	key1 := prefix + "a"
	key2 := prefix + "b"

	if err := kvRepo.Set(ctx, &jetmodel.Kv{Key: key1, Value: []byte("val1")}); err != nil {
		t.Fatalf("Set key1 failed: %v", err)
	}
	if err := kvRepo.Set(ctx, &jetmodel.Kv{Key: key2, Value: []byte("val2")}); err != nil {
		t.Fatalf("Set key2 failed: %v", err)
	}

	if err := kvRepo.DeletePrefix(ctx, prefix); err != nil {
		t.Fatalf("DeletePrefix failed: %v", err)
	}

	_, err1 := kvRepo.Get(ctx, key1)
	if !errors.Is(err1, repositories.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for key1, got %v", err1)
	}
	_, err2 := kvRepo.Get(ctx, key2)
	if !errors.Is(err2, repositories.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for key2, got %v", err2)
	}
}

func TestKVIterate(t *testing.T) {
	s := newHarness(t)
	ctx := context.Background()

	kvRepo := repositories.NewJetKVRepository(s.pool)

	prefix := "test-kv-iterate-"
	key1 := prefix + "x"
	key2 := prefix + "y"

	if err := kvRepo.Set(ctx, &jetmodel.Kv{Key: key1, Value: []byte("val1")}); err != nil {
		t.Fatalf("Set key1 failed: %v", err)
	}
	if err := kvRepo.Set(ctx, &jetmodel.Kv{Key: key2, Value: []byte("val2")}); err != nil {
		t.Fatalf("Set key2 failed: %v", err)
	}

	visited := make(map[string]bool)
	err := kvRepo.Iterate(ctx, prefix, func(key string, value []byte) error {
		visited[key] = true
		return nil
	})
	if err != nil {
		t.Fatalf("Iterate failed: %v", err)
	}

	if !visited[key1] {
		t.Errorf("key %s was not visited", key1)
	}
	if !visited[key2] {
		t.Errorf("key %s was not visited", key2)
	}
}
