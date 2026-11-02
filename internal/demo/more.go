package demo

import (
	"context"
	"encoding/json"

	"github.com/openpak/openverse/internal/store"
)

func (s *Store) Yeahed(_ context.Context, viewer, id string, p store.Page) ([]store.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.topLevel(func(x store.Post) bool { return s.yeahs[x.ID][id] }, viewer, false, p), nil
}

func (s *Store) UserActivity(ctx context.Context, viewer, id string, p store.Page) ([]store.ActivityItem, error) {
	all, _ := s.Activity(ctx, viewer, "everyone", store.Page{Limit: 50})
	var out []store.ActivityItem
	for _, a := range all {
		if a.Person.ID == id {
			out = append(out, a)
		}
	}
	return window(out, p), nil
}

func (s *Store) Relations(_ context.Context, _, id, kind string, p store.Page) ([]store.Person, []store.Community, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var people []store.Person
	for _, pid := range []string{"1000000005", "1000000012", "1000000031", "1800000044"} {
		if pid != id {
			people = append(people, s.person(pid))
		}
	}
	if kind == "friends" {
		people = people[:1]
	}
	var coms []store.Community
	if kind == "following" {
		coms = []store.Community{s.com("101"), s.com("102")}
	}
	return window(people, p), coms, nil
}

func (s *Store) TopicPosts(_ context.Context, viewer, tag string, p store.Page) ([]store.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.topLevel(func(x store.Post) bool { return x.Topic == tag }, viewer, false, p), nil
}

func (s *Store) SubCommunities(_ context.Context, _, parent string) ([]store.Community, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if parent == "102" {
		return []store.Community{s.com("112")}, nil
	}
	return nil, nil
}

func (s *Store) SetSpoiler(_ context.Context, viewer, post string, on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.posts {
		if s.posts[i].ID == post {
			if s.posts[i].Author.ID != viewer {
				return store.ErrForbidden
			}
			s.posts[i].Spoiler = on
			return nil
		}
	}
	return store.ErrNotFound
}

func (s *Store) DeleteActivity(context.Context, string, string) error { return nil }

func (s *Store) Export(context.Context, string) ([]byte, error) {
	return json.MarshalIndent(map[string]any{"pid": Me, "posts": len(s.posts)}, "", "  ")
}

func (s *Store) DeviceLinks(context.Context, string) ([]store.DeviceLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]store.DeviceLink(nil), s.devices...), nil
}

func (s *Store) RevokeDevice(_ context.Context, _, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, d := range s.devices {
		if d.ID == id {
			s.devices = append(s.devices[:i], s.devices[i+1:]...)
			return nil
		}
	}
	return store.ErrNotFound
}

func (s *Store) Popular(_ context.Context, _ string, n int) ([]store.Community, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Community
	for _, c := range s.communities {
		if !c.General && c.Parent == "" && c.Posts > 1500 && len(out) < n {
			out = append(out, c)
		}
	}
	return out, nil
}
