package miiverse

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/openpak/openverse/internal/store"
)

// Popular is the ten busiest communities over the last seven days, counted from the newest 500 posts
// across OpenVerse. Cached for five minutes.
type popularCache struct {
	mu    sync.Mutex
	at    time.Time
	count map[string]int // posts in the last seven days, per game community
}

func (c *Client) Popular(ctx context.Context, viewer string, n int) ([]store.Community, error) {
	pc := &c.popular
	pc.mu.Lock()
	stale := time.Since(pc.at) > 5*time.Minute
	pc.mu.Unlock()
	if stale && viewer != "" {
		var out wireList[wirePost]
		if err := c.do(ctx, http.MethodGet, "/activity-feed/fresh"+q(store.Page{Limit: 500}, nil), viewer, nil, &out); err != nil {
			return nil, err
		}
		count := map[string]int{}
		cutoff := time.Now().Add(-7 * 24 * time.Hour)
		for _, p := range out.Items {
			if p.Community != nil && p.CreatedAt.After(cutoff) {
				count[p.Community.ID]++
			}
		}
		pc.mu.Lock()
		pc.count, pc.at = count, time.Now()
		pc.mu.Unlock()
	}
	pc.mu.Lock()
	count := map[string]int{}
	for id, n := range pc.count {
		count[id] = n
	}
	pc.mu.Unlock()
	if len(count) == 0 {
		return nil, nil
	}
	all, _, err := c.Communities(ctx, store.Page{Limit: 600})
	if err != nil {
		return nil, err
	}
	byID := map[string]store.Community{}
	for _, c := range all {
		byID[c.ID] = c
	}
	// A game counts for its family: the family is what the tile shows.
	rolled := map[string]int{}
	for id, n := range count {
		if g, ok := byID[id]; ok && g.Parent != "" {
			id = g.Parent
		}
		rolled[id] += n
	}
	ids := make([]string, 0, len(rolled))
	for id := range rolled {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if rolled[ids[i]] != rolled[ids[j]] {
			return rolled[ids[i]] > rolled[ids[j]]
		}
		return ids[i] < ids[j]
	})
	var res []store.Community
	for _, id := range ids {
		if c, ok := byID[id]; ok && !c.General {
			res = append(res, c)
			if len(res) == n {
				break
			}
		}
	}
	return res, nil
}
