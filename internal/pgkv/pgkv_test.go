package pgkv

import (
	"context"
	"os"
	"testing"
)

// Needs a throwaway Postgres: OPENVERSE_TEST_DB=postgres://...
func TestStore(t *testing.T) {
	url := os.Getenv("OPENVERSE_TEST_DB")
	if url == "" {
		t.Skip("OPENVERSE_TEST_DB is not set")
	}
	ctx := context.Background()
	s, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Set(ctx, "verse-lite:code:ABC234", `{"a":1}`, 60); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := s.Get(ctx, "verse-lite:code:ABC234"); err != nil || !ok || v != `{"a":1}` {
		t.Fatalf("get: %q %v %v", v, ok, err)
	}
	if _, ok, _ := s.Get(ctx, "nope"); ok {
		t.Fatal("unknown key found")
	}
	_ = s.Set(ctx, "old", "x", 60)
	_, _ = s.pool.Exec(ctx, "UPDATE ui_kv SET expires_at = now() - interval '1 second' WHERE key = 'old'")
	if _, ok, _ := s.Get(ctx, "old"); ok {
		t.Fatal("expired key found")
	}
	if n, err := s.Sweep(ctx); err != nil || n < 1 {
		t.Fatalf("sweep %d %v", n, err)
	}
	_ = s.HSet(ctx, "openverse:names", "1", "Ada")
	_ = s.HSet(ctx, "openverse:names", "1", "Ada B")
	m, err := s.HMGet(ctx, "openverse:names", []string{"1", "2"})
	if err != nil || m["1"] != "Ada B" || len(m) != 1 {
		t.Fatalf("hmget %v %v", m, err)
	}
}
