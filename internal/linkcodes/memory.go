package linkcodes

import "context"

// Memory is a KV in memory, for tests and the demo.
type Memory struct{ m map[string]string }

// NewMemory returns an empty Memory.
func NewMemory() *Memory { return &Memory{m: map[string]string{}} }

// Get implements KV.
func (m *Memory) Get(_ context.Context, k string) (string, bool, error) {
	v, ok := m.m[k]
	return v, ok, nil
}

// Set implements KV.
func (m *Memory) Set(_ context.Context, k, v string, _ int) error { m.m[k] = v; return nil }
