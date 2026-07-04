package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// LocalStore is the machine-local cache tier: content-addressed tar.zst
// archives on the agent's NVMe. A hit skips the network entirely — the
// dominant cache economics win of persistent machines. Evicts least-recently-
// used entries past MaxBytes.
type LocalStore struct {
	// Dir is the store root (e.g. /var/lib/flint-agent/cache).
	Dir string
	// MaxBytes caps the store size (0 = 20 GiB).
	MaxBytes int64

	mu  sync.Mutex
	idx *localIndex
}

// localIndex tracks entries for prefix lookup and LRU eviction. Persisted as
// JSON so restarts keep working sets warm.
type localIndex struct {
	Entries map[string]*localEntry `json:"entries"` // cache key → entry
}

type localEntry struct {
	File     string    `json:"file"`
	Size     int64     `json:"size"`
	LastUsed time.Time `json:"lastUsed"`
}

const defaultLocalCacheBytes = 20 << 30

// NewLocal opens (or initialises) a local store rooted at dir.
func NewLocal(dir string, maxBytes int64) (*LocalStore, error) {
	if maxBytes <= 0 {
		maxBytes = defaultLocalCacheBytes
	}
	if err := os.MkdirAll(filepath.Join(dir, "objects"), 0o700); err != nil {
		return nil, err
	}
	s := &LocalStore{Dir: dir, MaxBytes: maxBytes}
	s.idx = s.loadIndex()
	return s, nil
}

// Restore extracts a locally cached archive. (true, nil) on hit.
func (s *LocalStore) Restore(ctx context.Context, root, key string, _ []string) (bool, error) {
	path, ok := s.lookup(key)
	if !ok {
		return false, nil
	}
	f, err := os.Open(path)
	if err != nil {
		// Index/file drift: drop the entry, report miss.
		s.drop(key)
		return false, nil
	}
	defer f.Close() //nolint:errcheck
	if err := extract(root, f); err != nil {
		s.drop(key)
		return false, err
	}
	log.Debug().Str("key", key).Msg("cache: local hit")
	return true, nil
}

// RestoreWithFallback tries the exact key, then each restore key as a prefix
// (lexicographically last local match wins — matching the S3 tier's rule).
func (s *LocalStore) RestoreWithFallback(ctx context.Context, root, key string, restoreKeys []string, paths []string) (string, error) {
	if hit, err := s.Restore(ctx, root, key, paths); err != nil {
		return "", err
	} else if hit {
		return key, nil
	}
	for _, prefix := range restoreKeys {
		if match := s.bestPrefixMatch(prefix); match != "" {
			if hit, err := s.Restore(ctx, root, match, paths); err != nil {
				return "", err
			} else if hit {
				return match, nil
			}
		}
	}
	return "", nil
}

// Save compresses paths into the store under key, evicting to the watermark
// first so the write always fits.
func (s *LocalStore) Save(ctx context.Context, root, key string, paths []string) error {
	file := s.objectPath(key)
	tmp := file + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := compress(root, paths, f); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	info, err := os.Stat(tmp)
	if err != nil {
		return err
	}
	s.evictFor(info.Size())
	if err := os.Rename(tmp, file); err != nil {
		return err
	}

	s.mu.Lock()
	s.idx.Entries[key] = &localEntry{File: file, Size: info.Size(), LastUsed: time.Now()}
	s.persistIndexLocked()
	s.mu.Unlock()
	return nil
}

// lookup resolves a key to its archive path, touching LRU on hit.
func (s *LocalStore) lookup(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.idx.Entries[key]
	if !ok {
		return "", false
	}
	e.LastUsed = time.Now()
	s.persistIndexLocked()
	return e.File, true
}

func (s *LocalStore) drop(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.idx.Entries[key]; ok {
		_ = os.Remove(e.File)
		delete(s.idx.Entries, key)
		s.persistIndexLocked()
	}
}

// bestPrefixMatch returns the lexicographically last key with the prefix.
func (s *LocalStore) bestPrefixMatch(prefix string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	best := ""
	for k := range s.idx.Entries {
		if strings.HasPrefix(k, prefix) && k > best {
			best = k
		}
	}
	return best
}

// evictFor frees space so `incoming` bytes fit under MaxBytes, LRU first.
func (s *LocalStore) evictFor(incoming int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var total int64
	type kv struct {
		key string
		e   *localEntry
	}
	entries := make([]kv, 0, len(s.idx.Entries))
	for k, e := range s.idx.Entries {
		total += e.Size
		entries = append(entries, kv{k, e})
	}
	if total+incoming <= s.MaxBytes {
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].e.LastUsed.Before(entries[j].e.LastUsed) })
	for _, kv := range entries {
		if total+incoming <= s.MaxBytes {
			break
		}
		_ = os.Remove(kv.e.File)
		total -= kv.e.Size
		delete(s.idx.Entries, kv.key)
		log.Debug().Str("key", kv.key).Int64("size", kv.e.Size).Msg("cache: local eviction")
	}
	s.persistIndexLocked()
}

func (s *LocalStore) objectPath(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(s.Dir, "objects", hex.EncodeToString(sum[:])+".tar.zst")
}

func (s *LocalStore) indexPath() string { return filepath.Join(s.Dir, "index.json") }

func (s *LocalStore) loadIndex() *localIndex {
	idx := &localIndex{Entries: map[string]*localEntry{}}
	data, err := os.ReadFile(s.indexPath())
	if err == nil {
		_ = json.Unmarshal(data, idx)
		if idx.Entries == nil {
			idx.Entries = map[string]*localEntry{}
		}
	}
	return idx
}

func (s *LocalStore) persistIndexLocked() {
	data, err := json.Marshal(s.idx)
	if err != nil {
		return
	}
	_ = os.WriteFile(s.indexPath(), data, 0o600)
}
