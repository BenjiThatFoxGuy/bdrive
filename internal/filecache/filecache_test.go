package filecache

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testConfig(t *testing.T, dir string) Config {
	t.Helper()
	return Config{
		MemoryEnabled:     true,
		MemoryMaxBytes:    1024,
		MemoryMaxFileByte: 256,
		MemoryTTL:         time.Minute,
		DiskEnabled:       true,
		DiskPath:          dir,
		DiskMaxBytes:      4096,
		DiskMaxFileByte:   0,
		DiskTTL:           time.Minute,
	}
}

func store(t *testing.T, c *Cache, id string, size int64, body []byte) {
	t.Helper()
	w, err := c.Store(id, size)
	if err != nil {
		t.Fatalf("Store(%s) = %v", id, err)
	}
	if _, err := w.Write(body); err != nil {
		t.Fatalf("Write(%s) = %v", id, err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close(%s) = %v", id, err)
	}
}

func readAll(t *testing.T, e *Entry) []byte {
	t.Helper()
	data, err := io.ReadAll(e)
	if err != nil {
		t.Fatalf("ReadAll = %v", err)
	}
	return data
}

func TestMemoryStoreOpenSeek(t *testing.T) {
	c, err := New(testConfig(t, t.TempDir()), nil)
	if err != nil {
		t.Fatal(err)
	}
	body := bytes.Repeat([]byte("abcdef"), 20)
	store(t, c, "file-1", int64(len(body)), body)

	e, ok := c.Open("file-1")
	if !ok {
		t.Fatal("expected a cache hit")
	}
	if e.Tier != "memory" {
		t.Fatalf("tier = %s, want memory", e.Tier)
	}
	if e.Size != int64(len(body)) {
		t.Fatalf("size = %d, want %d", e.Size, len(body))
	}

	if _, err := e.Seek(6, io.SeekStart); err != nil {
		t.Fatalf("Seek = %v", err)
	}
	got := make([]byte, 6)
	if _, err := io.ReadFull(e, got); err != nil {
		t.Fatalf("ReadFull = %v", err)
	}
	if string(got) != "abcdef" {
		t.Fatalf("read after seek = %q", got)
	}
}

func TestShortWriteIsNotPublished(t *testing.T) {
	c, err := New(testConfig(t, t.TempDir()), nil)
	if err != nil {
		t.Fatal(err)
	}
	w, err := c.Store("partial", 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("only ten!")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close = %v", err)
	}
	if _, ok := c.Open("partial"); ok {
		t.Fatal("partial entry must not be published")
	}
}

func TestMemoryEvictsLeastRecentlyUsed(t *testing.T) {
	cfg := testConfig(t, t.TempDir())
	cfg.MemoryMaxBytes = 100 // three 40-byte files, only two fit
	c, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}

	store(t, c, "a", 40, bytes.Repeat([]byte("a"), 40))
	store(t, c, "b", 40, bytes.Repeat([]byte("b"), 40))
	// Touch "a" so "b" becomes the oldest.
	if _, ok := c.Open("a"); !ok {
		t.Fatal("expected a to be cached")
	}
	store(t, c, "c", 40, bytes.Repeat([]byte("c"), 40))

	if _, ok := c.Open("b"); ok {
		t.Fatal("b should have been evicted")
	}
	for _, id := range []string{"a", "c"} {
		if _, ok := c.Open(id); !ok {
			t.Fatalf("%s should still be cached", id)
		}
	}
	if s := c.Stats().Memory; s.Bytes > cfg.MemoryMaxBytes {
		t.Fatalf("memory bytes = %d, over budget %d", s.Bytes, cfg.MemoryMaxBytes)
	}
}

func TestTierRouting(t *testing.T) {
	cfg := testConfig(t, t.TempDir())
	cfg.MemoryMaxFileByte = 8
	c, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}

	small := []byte("tiny")
	store(t, c, "small", int64(len(small)), small)
	large := bytes.Repeat([]byte("x"), 500)
	store(t, c, "large", int64(len(large)), large)

	if e, ok := c.Open("small"); !ok || e.Tier != "memory" {
		t.Fatalf("small: hit=%v tier=%s, want memory", ok, e.Tier)
	}
	if e, ok := c.Open("large"); !ok || e.Tier != "disk" {
		t.Fatalf("large: hit=%v tier=%s, want disk", ok, e.Tier)
	}
}

func TestTooLargeIsSkipped(t *testing.T) {
	cfg := testConfig(t, t.TempDir())
	cfg.MemoryMaxFileByte = 8
	cfg.DiskMaxBytes = 16
	c, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Store("huge", 100); err != ErrSkipped {
		t.Fatalf("Store = %v, want ErrSkipped", err)
	}
	if c.Enabled() != true {
		t.Fatal("cache should still be enabled")
	}
}

func TestDiskSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(t, dir)
	c, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	body := bytes.Repeat([]byte("disk-body-"), 40) // 400 bytes, over the memory cap
	store(t, c, "persist", int64(len(body)), body)

	// A new cache over the same directory must rebuild its index from disk.
	c2, err := New(testConfig(t, dir), nil)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := c2.Open("persist")
	if !ok {
		t.Fatal("expected the entry to survive a restart")
	}
	if got := readAll(t, e); !bytes.Equal(got, body) {
		t.Fatalf("round-tripped body differs: %d bytes", len(got))
	}
}

func TestDiskEviction(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(t, dir)
	cfg.MemoryMaxFileByte = 0 // force everything to disk
	cfg.DiskMaxBytes = 100
	c, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}

	store(t, c, "one", 60, bytes.Repeat([]byte("1"), 60))
	store(t, c, "two", 60, bytes.Repeat([]byte("2"), 60))

	if s := c.Stats().Disk; s.Bytes > cfg.DiskMaxBytes {
		t.Fatalf("disk bytes = %d, over budget %d", s.Bytes, cfg.DiskMaxBytes)
	}
	if _, ok := c.Open("one"); ok {
		t.Fatal("oldest disk entry should have been evicted")
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 blob on disk, found %d", len(files))
	}
}

func TestDeleteAndFlush(t *testing.T) {
	c, err := New(testConfig(t, t.TempDir()), nil)
	if err != nil {
		t.Fatal(err)
	}
	store(t, c, "gone", 4, []byte("gone"))
	store(t, c, "stays", 5, []byte("stays"))

	c.Delete("gone")
	if _, ok := c.Open("gone"); ok {
		t.Fatal("Delete did not drop the entry")
	}
	if _, ok := c.Open("stays"); !ok {
		t.Fatal("Delete removed the wrong entry")
	}

	c.Flush()
	if _, ok := c.Open("stays"); ok {
		t.Fatal("Flush did not empty the cache")
	}
}

func TestTTLExpiry(t *testing.T) {
	cfg := testConfig(t, t.TempDir())
	cfg.MemoryTTL = 30 * time.Millisecond
	c, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	store(t, c, "ephemeral", 3, []byte("abc"))
	time.Sleep(60 * time.Millisecond)
	if _, ok := c.Open("ephemeral"); ok {
		t.Fatal("expired entry should not be served")
	}
}

func TestDisabledCacheIsSkipped(t *testing.T) {
	c, err := New(Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Enabled() {
		t.Fatal("an all-disabled cache should report Enabled() == false")
	}
	if _, err := c.Store("x", 1); err != ErrSkipped {
		t.Fatalf("Store = %v, want ErrSkipped", err)
	}
	if _, ok := c.Open("x"); ok {
		t.Fatal("disabled cache must never hit")
	}
	c.Delete("x")
	c.Flush()
	if s := c.Stats(); s.Memory.Bytes != 0 || s.Disk.Bytes != 0 {
		t.Fatalf("disabled cache reported usage: %+v", s)
	}
}

func TestDiskPathIsNotCreatedWhenDisabled(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested")
	c, err := New(Config{MemoryEnabled: true, MemoryMaxBytes: 64, MemoryMaxFileByte: 64, MemoryTTL: time.Minute}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("disk tier should not have been created: %v", err)
	}
	if c.disk != nil {
		t.Fatal("disk tier should be nil when disabled")
	}
}
