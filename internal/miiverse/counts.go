package miiverse

import (
	"context"
	"net/url"
	"sync"
	"time"
)

// The community list endpoint sends no post count and /stats answers for one community at a time,
// so the counts are swept in the background (a few at once) and cached for a few minutes. The first
// request after a start waits a moment for the first sweep; later ones never wait.
const (
	countsTTL   = 5 * time.Minute
	countsFirst = 4 * time.Second
	countsPar   = 8
)

type countCache struct {
	mu      sync.Mutex
	at      time.Time
	counts  map[string]int
	running chan struct{} // closed when the running sweep ends; nil when none
}

func (c *Client) postCounts(ctx context.Context, ids []string) map[string]int {
	cc := &c.counts
	cc.mu.Lock()
	fresh := cc.counts != nil && time.Since(cc.at) < countsTTL
	first := cc.counts == nil
	var wait chan struct{}
	if !fresh {
		if cc.running == nil {
			cc.running = make(chan struct{})
			go c.sweep(ids, cc.running)
		}
		wait = cc.running
	}
	cc.mu.Unlock()
	if first && wait != nil {
		select {
		case <-wait:
		case <-time.After(countsFirst):
		case <-ctx.Done():
		}
	}
	cc.mu.Lock()
	defer cc.mu.Unlock()
	out := make(map[string]int, len(cc.counts))
	for k, v := range cc.counts {
		out[k] = v
	}
	return out
}

func (c *Client) sweep(ids []string, done chan struct{}) {
	got := map[string]int{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, countsPar)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	for _, id := range ids {
		wg.Add(1)
		sem <- struct{}{}
		go func(id string) {
			defer wg.Done()
			defer func() { <-sem }()
			var st struct {
				TotalPosts int `json:"totalPosts"`
			}
			if err := c.getOne(ctx, "/communities/"+url.PathEscape(id)+"/stats", "", &st); err == nil {
				mu.Lock()
				got[id] = st.TotalPosts
				mu.Unlock()
			}
		}(id)
	}
	wg.Wait()
	cc := &c.counts
	cc.mu.Lock()
	if len(got) > 0 {
		if cc.counts == nil {
			cc.counts = map[string]int{}
		}
		for k, v := range got {
			cc.counts[k] = v
		}
		cc.at = time.Now()
	}
	cc.running = nil
	cc.mu.Unlock()
	close(done)
}
