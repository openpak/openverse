// Package names gives every author on the web their whole display name. The console limits a name
// to what a Mii can carry (the API stores "Switch Tes" for "Switch Test User"), but the OpenPak
// account's display name is what a person chose. Each sign-in to OpenVerse records the account's
// display name under the person's OpenVerse number; pages read the names in one batch and cache
// them. A person who has never signed in on the web keeps the console name.
package names

import (
	"context"
	"sync"
	"time"
)

const key = "openverse:names"

// Backend is the hash in Redis (or memory).
type Backend interface {
	HSet(ctx context.Context, key, field, value string) error
	HMGet(ctx context.Context, key string, fields []string) (map[string]string, error)
}

// Book looks names up and records them.
type Book struct {
	b     Backend
	mu    sync.Mutex
	cache map[string]entry
}

type entry struct {
	name string
	at   time.Time
}

const ttl = 10 * time.Minute

// New returns a Book over a backend; nil gives a Book that knows nothing.
func New(b Backend) *Book { return &Book{b: b, cache: map[string]entry{}} }

// Put records the display name of the person with this OpenVerse number.
func (k *Book) Put(ctx context.Context, id, name string) {
	if k == nil || k.b == nil || id == "" || name == "" {
		return
	}
	if err := k.b.HSet(ctx, key, id, name); err == nil {
		k.mu.Lock()
		k.cache[id] = entry{name, time.Now()}
		k.mu.Unlock()
	}
}

// Lookup returns the whole names it knows for the numbers, in one backend call for the misses.
func (k *Book) Lookup(ctx context.Context, ids []string) map[string]string {
	out := map[string]string{}
	if k == nil || k.b == nil {
		return out
	}
	var miss []string
	seen := map[string]bool{}
	k.mu.Lock()
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if e, ok := k.cache[id]; ok && time.Since(e.at) < ttl {
			if e.name != "" {
				out[id] = e.name
			}
			continue
		}
		miss = append(miss, id)
	}
	k.mu.Unlock()
	if len(miss) == 0 {
		return out
	}
	cctx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	got, err := k.b.HMGet(cctx, key, miss)
	if err != nil {
		return out // names are a nicety: never fail a page for them
	}
	k.mu.Lock()
	for _, id := range miss {
		k.cache[id] = entry{got[id], time.Now()}
		if got[id] != "" {
			out[id] = got[id]
		}
	}
	if len(k.cache) > 20000 {
		k.cache = map[string]entry{}
	}
	k.mu.Unlock()
	return out
}

// Memory is a Backend in memory, for tests and the demo.
type Memory struct {
	mu sync.Mutex
	m  map[string]string
}

// NewMemory returns an empty Memory.
func NewMemory() *Memory { return &Memory{m: map[string]string{}} }

// HSet implements Backend.
func (m *Memory) HSet(_ context.Context, _, f, v string) error {
	m.mu.Lock()
	m.m[f] = v
	m.mu.Unlock()
	return nil
}

// HMGet implements Backend.
func (m *Memory) HMGet(_ context.Context, _ string, fields []string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]string{}
	for _, f := range fields {
		if v, ok := m.m[f]; ok {
			out[f] = v
		}
	}
	return out, nil
}
