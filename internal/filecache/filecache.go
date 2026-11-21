// Package filecache stores decrypted file contents locally so a file that is
// requested again is not fetched from Telegram a second time.
//
// Entries are whole files, keyed by file ID, and land in one of two tiers:
// memory for small files and disk for everything larger. Each tier has a byte
// budget and a TTL, and evicts least-recently-used entries when the budget is
// exceeded. Nothing is mounted by default - the disk tier is a plain directory,
// so pointing it at a bind mount keeps the data off the container layer.
package filecache

import (
	"bytes"
	"container/list"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tgdrive/teldrive/internal/logging"
	"go.uber.org/zap"
)

// ErrSkipped is returned by Store when a file is too large for every enabled
// tier. It is not a failure: the caller simply streams without caching.
var ErrSkipped = errors.New("filecache: file too large to cache")

// Config describes the two tiers. Sizes are bytes, TTLs are durations.
type Config struct {
	MemoryEnabled     bool
	MemoryMaxBytes    int64
	MemoryMaxFileByte int64
	MemoryTTL         time.Duration

	DiskEnabled     bool
	DiskPath        string
	DiskMaxBytes    int64
	DiskMaxFileByte int64
	DiskTTL         time.Duration
}

// Entry is a cached file, readable and seekable so range requests can be
// served straight out of the cache.
type Entry struct {
	FileID string
	Size   int64
	Tier   string
	io.ReadSeekCloser
}

// TierStats reports what one tier is currently holding.
type TierStats struct {
	Bytes   int64
	Entries int
	// Evictions is the running count of entries dropped for budget or TTL.
	Evictions int64
}

// Stats reports per-tier usage.
type Stats struct {
	Memory TierStats
	Disk   TierStats
}

// Cache is the tiered file store.
type Cache struct {
	cfg  Config
	mem  *memTier
	disk *diskTier
	lg   *zap.Logger
}

// New builds a cache from cfg. Disabled tiers are nil and skipped. When both
// tiers are disabled the returned cache is a no-op that always skips.
func New(cfg Config, lg *zap.Logger) (*Cache, error) {
	if lg == nil {
		lg = logging.Component("FILECACHE")
	}
	c := &Cache{cfg: cfg, lg: lg}

	if cfg.MemoryEnabled && cfg.MemoryMaxBytes > 0 {
		c.mem = newMemTier(cfg.MemoryMaxBytes, cfg.MemoryMaxFileByte, cfg.MemoryTTL)
	}
	if cfg.DiskEnabled && cfg.DiskMaxBytes > 0 && cfg.DiskPath != "" {
		d, err := newDiskTier(cfg.DiskPath, cfg.DiskMaxBytes, cfg.DiskMaxFileByte, cfg.DiskTTL)
		if err != nil {
			return nil, err
		}
		c.disk = d
	}
	return c, nil
}

// Enabled reports whether any tier can accept files.
func (c *Cache) Enabled() bool { return c.mem != nil || c.disk != nil }

// Start runs the TTL janitor until ctx is cancelled. Both tiers are swept once
// a minute; entries are also checked lazily on access.
func (c *Cache) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if c.mem != nil {
					c.mem.evictExpired()
				}
				if c.disk != nil {
					c.disk.evictExpired()
				}
			}
		}
	}()
}

// Open returns the cached entry for fileID, promoting its position in the LRU.
// The second return value is false when the file is not cached.
func (c *Cache) Open(fileID string) (*Entry, bool) {
	if c == nil || fileID == "" {
		return nil, false
	}
	if c.mem != nil {
		if data, ok := c.mem.get(fileID); ok {
			return &Entry{FileID: fileID, Size: int64(len(data)), Tier: "memory",
				ReadSeekCloser: readSeekNopCloser{bytes.NewReader(data)}}, true
		}
	}
	if c.disk != nil {
		if f, size, ok := c.disk.get(fileID); ok {
			return &Entry{FileID: fileID, Size: size, Tier: "disk", ReadSeekCloser: f}, true
		}
	}
	return nil, false
}

// Exists reports whether fileID is cached in any tier.
func (c *Cache) Exists(fileID string) bool {
	_, ok := c.Open(fileID)
	return ok
}

// Store begins caching fileID. The returned writer receives the file's
// decrypted bytes; the entry becomes visible only when the writer is closed
// after exactly size bytes have been written. A short write discards the
// partial entry rather than publishing a truncated file.
//
// Store returns ErrSkipped when no enabled tier can hold a file of that size.
func (c *Cache) Store(fileID string, size int64) (io.WriteCloser, error) {
	if c == nil || !c.Enabled() || fileID == "" || size <= 0 {
		return nil, ErrSkipped
	}
	switch {
	case c.mem != nil && size <= c.cfg.MemoryMaxFileByte:
		return c.mem.store(fileID, size), nil
	case c.disk != nil && size <= c.cfg.DiskMaxBytes && (c.cfg.DiskMaxFileByte <= 0 || size <= c.cfg.DiskMaxFileByte):
		return c.disk.store(fileID, size)
	default:
		return nil, ErrSkipped
	}
}

// Delete drops fileID from every tier. Used when a file is removed or replaced.
func (c *Cache) Delete(fileID string) {
	if c == nil || fileID == "" {
		return
	}
	if c.mem != nil {
		c.mem.delete(fileID)
	}
	if c.disk != nil {
		c.disk.delete(fileID)
	}
}

// Flush empties every tier.
func (c *Cache) Flush() {
	if c == nil {
		return
	}
	if c.mem != nil {
		c.mem.flush()
	}
	if c.disk != nil {
		c.disk.flush()
	}
}

// Stats reports per-tier usage.
func (c *Cache) Stats() Stats {
	var s Stats
	if c == nil {
		return s
	}
	if c.mem != nil {
		s.Memory = c.mem.stats()
	}
	if c.disk != nil {
		s.Disk = c.disk.stats()
	}
	return s
}

// readSeekNopCloser turns a bytes.Reader into an Entry reader.
type readSeekNopCloser struct{ *bytes.Reader }

func (readSeekNopCloser) Close() error { return nil }

// --- memory tier ---

type memItem struct {
	id         string
	data       []byte
	expires    time.Time
	lastAccess time.Time
}

type memTier struct {
	maxBytes    int64
	maxFileByte int64
	ttl         time.Duration

	mu        sync.Mutex
	bytes     int64
	evictions int64
	items     map[string]*list.Element
	lru       *list.List
}

func newMemTier(maxBytes, maxFileByte int64, ttl time.Duration) *memTier {
	return &memTier{
		maxBytes: maxBytes, maxFileByte: maxFileByte, ttl: ttl,
		items: map[string]*list.Element{}, lru: list.New(),
	}
}

type memWriter struct {
	t    *memTier
	id   string
	size int64
	buf  []byte
	done bool
}

func (t *memTier) store(id string, size int64) io.WriteCloser {
	return &memWriter{t: t, id: id, size: size}
}

func (w *memWriter) Write(p []byte) (int, error) {
	if w.done {
		return 0, errors.New("filecache: write after close")
	}
	if int64(len(w.buf)+len(p)) > w.size {
		return 0, fmt.Errorf("filecache: write exceeds declared size %d", w.size)
	}
	w.buf = append(w.buf, p...)
	return len(p), nil
}

func (w *memWriter) Close() error {
	if w.done {
		return nil
	}
	w.done = true
	if int64(len(w.buf)) != w.size {
		// Truncated download: do not publish a partial file.
		return nil
	}
	w.t.put(w.id, w.buf)
	return nil
}

func (t *memTier) put(id string, data []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	if el, ok := t.items[id]; ok {
		t.lru.Remove(el)
		t.bytes -= int64(len(el.Value.(*memItem).data))
		delete(t.items, id)
	}
	item := &memItem{id: id, data: data, expires: now.Add(t.ttl), lastAccess: now}
	t.items[id] = t.lru.PushFront(item)
	t.bytes += int64(len(data))

	for t.bytes > t.maxBytes {
		back := t.lru.Back()
		if back == nil {
			break
		}
		old := back.Value.(*memItem)
		t.lru.Remove(back)
		delete(t.items, old.id)
		t.bytes -= int64(len(old.data))
		t.evictions++
	}
}

func (t *memTier) get(id string) ([]byte, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	el, ok := t.items[id]
	if !ok {
		return nil, false
	}
	item := el.Value.(*memItem)
	if time.Now().After(item.expires) {
		t.remove(el, item)
		return nil, false
	}
	t.lru.MoveToFront(el)
	item.lastAccess = time.Now()
	return item.data, true
}

func (t *memTier) remove(el *list.Element, item *memItem) {
	t.lru.Remove(el)
	delete(t.items, item.id)
	t.bytes -= int64(len(item.data))
	t.evictions++
}

func (t *memTier) delete(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if el, ok := t.items[id]; ok {
		t.remove(el, el.Value.(*memItem))
	}
}

func (t *memTier) flush() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.items = map[string]*list.Element{}
	t.lru.Init()
	t.bytes = 0
}

func (t *memTier) evictExpired() {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	for el := t.lru.Back(); el != nil; {
		prev := el.Prev()
		item := el.Value.(*memItem)
		if now.After(item.expires) {
			t.remove(el, item)
		}
		el = prev
	}
}

func (t *memTier) stats() TierStats {
	t.mu.Lock()
	defer t.mu.Unlock()
	return TierStats{Bytes: t.bytes, Entries: len(t.items), Evictions: t.evictions}
}

// --- disk tier ---

type diskItem struct {
	id         string
	path       string
	size       int64
	expires    time.Time
	lastAccess time.Time
}

type diskTier struct {
	path        string
	maxBytes    int64
	maxFileByte int64
	ttl         time.Duration

	mu        sync.Mutex
	bytes     int64
	evictions int64
	items     map[string]*list.Element
	lru       *list.List
}

func newDiskTier(path string, maxBytes, maxFileByte int64, ttl time.Duration) (*diskTier, error) {
	if err := os.MkdirAll(path, 0o750); err != nil {
		return nil, fmt.Errorf("filecache: create %s: %w", path, err)
	}
	t := &diskTier{
		path: path, maxBytes: maxBytes, maxFileByte: maxFileByte, ttl: ttl,
		items: map[string]*list.Element{}, lru: list.New(),
	}
	if err := t.scan(); err != nil {
		return nil, err
	}
	return t, nil
}

// scan rebuilds the index from what is on disk and drops leftovers from
// interrupted writes.
func (t *diskTier) scan() error {
	entries, err := os.ReadDir(t.path)
	if err != nil {
		return fmt.Errorf("filecache: read %s: %w", t.path, err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasSuffix(name, ".tmp") {
			if strings.HasSuffix(name, ".tmp") {
				_ = os.Remove(filepath.Join(t.path, name))
			}
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		id, ok := t.idFor(name)
		if !ok {
			// Not one of ours (or corrupted); leave it alone.
			continue
		}
		item := &diskItem{id: id, path: filepath.Join(t.path, name), size: info.Size(),
			expires: info.ModTime().Add(t.ttl), lastAccess: info.ModTime()}
		t.items[id] = t.lru.PushFront(item)
		t.bytes += info.Size()
	}
	t.evictOverflow()
	return nil
}

// nameFor encodes the file ID into a filename that is safe on any filesystem.
// The encoding round-trips, so a restart can rebuild the index without sidecars.
func (t *diskTier) nameFor(id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(id)) + ".blob"
}

func (t *diskTier) idFor(name string) (string, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSuffix(name, ".blob"))
	if err != nil {
		return "", false
	}
	return string(raw), true
}

func (t *diskTier) store(id string, size int64) (io.WriteCloser, error) {
	final := filepath.Join(t.path, t.nameFor(id))
	tmp := final + fmt.Sprintf(".tmp-%d", time.Now().UnixNano())
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("filecache: create temp: %w", err)
	}
	return &diskWriter{t: t, id: id, size: size, f: f, tmp: tmp, final: final}, nil
}

type diskWriter struct {
	t       *diskTier
	id      string
	size    int64
	f       *os.File
	tmp     string
	final   string
	written int64
	done    bool
}

func (w *diskWriter) Write(p []byte) (int, error) {
	if w.done {
		return 0, errors.New("filecache: write after close")
	}
	n, err := w.f.Write(p)
	w.written += int64(n)
	if w.written > w.size {
		return n, fmt.Errorf("filecache: write exceeds declared size %d", w.size)
	}
	return n, err
}

func (w *diskWriter) Close() error {
	if w.done {
		return nil
	}
	w.done = true
	if err := w.f.Close(); err != nil {
		_ = os.Remove(w.tmp)
		return err
	}
	if w.written != w.size {
		_ = os.Remove(w.tmp)
		return nil
	}
	if err := os.Rename(w.tmp, w.final); err != nil {
		_ = os.Remove(w.tmp)
		return fmt.Errorf("filecache: publish: %w", err)
	}
	w.t.put(w.id, w.final, w.size)
	return nil
}

func (t *diskTier) put(id, path string, size int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	if el, ok := t.items[id]; ok {
		_ = os.Remove(el.Value.(*diskItem).path)
		t.lru.Remove(el)
		t.bytes -= el.Value.(*diskItem).size
		delete(t.items, id)
	}
	item := &diskItem{id: id, path: path, size: size, expires: now.Add(t.ttl), lastAccess: now}
	t.items[id] = t.lru.PushFront(item)
	t.bytes += size

	t.evictOverflowLocked()
}

// evictOverflow removes oldest entries until the budget is respected. Stale
// entries past their TTL are dropped on the way past.
func (t *diskTier) evictOverflow() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.evictOverflowLocked()
}

func (t *diskTier) evictOverflowLocked() {
	for t.bytes > t.maxBytes {
		back := t.lru.Back()
		if back == nil {
			break
		}
		item := back.Value.(*diskItem)
		_ = os.Remove(item.path)
		t.lru.Remove(back)
		delete(t.items, item.id)
		t.bytes -= item.size
		t.evictions++
	}
}

func (t *diskTier) get(id string) (*os.File, int64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	el, ok := t.items[id]
	if !ok {
		return nil, 0, false
	}
	item := el.Value.(*diskItem)
	if time.Now().After(item.expires) {
		t.remove(el, item)
		return nil, 0, false
	}
	f, err := os.Open(item.path)
	if err != nil {
		t.remove(el, item)
		return nil, 0, false
	}
	t.lru.MoveToFront(el)
	item.lastAccess = time.Now()
	return f, item.size, true
}

func (t *diskTier) remove(el *list.Element, item *diskItem) {
	t.lru.Remove(el)
	delete(t.items, item.id)
	t.bytes -= item.size
	t.evictions++
	_ = os.Remove(item.path)
}

func (t *diskTier) delete(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if el, ok := t.items[id]; ok {
		t.remove(el, el.Value.(*diskItem))
	}
}

func (t *diskTier) flush() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, el := range t.items {
		_ = os.Remove(el.Value.(*diskItem).path)
	}
	t.items = map[string]*list.Element{}
	t.lru.Init()
	t.bytes = 0
}

func (t *diskTier) evictExpired() {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	for el := t.lru.Back(); el != nil; {
		prev := el.Prev()
		item := el.Value.(*diskItem)
		if now.After(item.expires) {
			t.remove(el, item)
		}
		el = prev
	}
}

func (t *diskTier) stats() TierStats {
	t.mu.Lock()
	defer t.mu.Unlock()
	return TierStats{Bytes: t.bytes, Entries: len(t.items), Evictions: t.evictions}
}
