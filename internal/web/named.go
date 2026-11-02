package web

import (
	"context"

	"github.com/openpak/openverse/internal/names"
	"github.com/openpak/openverse/internal/store"
)

// namedStore swaps the console-limited names the API sends for the people's whole display names,
// for every author, actor and listed person. Anything without a recorded name is left as it is.
type namedStore struct {
	store.Store
	book *names.Book
}

func (n *namedStore) Media(ctx context.Context, key string) (string, []byte, error) {
	if m, ok := n.Store.(interface {
		Media(context.Context, string) (string, []byte, error)
	}); ok {
		return m.Media(ctx, key)
	}
	return "", nil, store.ErrNotFound
}

func (n *namedStore) fixPosts(ctx context.Context, posts []store.Post) []store.Post {
	ids := make([]string, 0, len(posts))
	for _, p := range posts {
		ids = append(ids, p.Author.ID)
	}
	full := n.book.Lookup(ctx, ids)
	for i := range posts {
		if v := full[posts[i].Author.ID]; v != "" {
			posts[i].Author.Name = v
		}
	}
	return posts
}

func (n *namedStore) fixPeople(ctx context.Context, people []store.Person) []store.Person {
	ids := make([]string, 0, len(people))
	for _, p := range people {
		ids = append(ids, p.ID)
	}
	full := n.book.Lookup(ctx, ids)
	for i := range people {
		if v := full[people[i].ID]; v != "" {
			people[i].Name = v
		}
	}
	return people
}

func (n *namedStore) CommunityPosts(ctx context.Context, v, id string, pop bool, p store.Page) ([]store.Post, error) {
	l, err := n.Store.CommunityPosts(ctx, v, id, pop, p)
	return n.fixPosts(ctx, l), err
}
func (n *namedStore) Feed(ctx context.Context, v, scope string, p store.Page) ([]store.Post, error) {
	l, err := n.Store.Feed(ctx, v, scope, p)
	return n.fixPosts(ctx, l), err
}
func (n *namedStore) Replies(ctx context.Context, v, id string, newest bool) ([]store.Post, error) {
	l, err := n.Store.Replies(ctx, v, id, newest)
	return n.fixPosts(ctx, l), err
}
func (n *namedStore) UserPosts(ctx context.Context, v, id string, p store.Page) ([]store.Post, error) {
	l, err := n.Store.UserPosts(ctx, v, id, p)
	return n.fixPosts(ctx, l), err
}
func (n *namedStore) Yeahed(ctx context.Context, v, id string, p store.Page) ([]store.Post, error) {
	l, err := n.Store.Yeahed(ctx, v, id, p)
	return n.fixPosts(ctx, l), err
}
func (n *namedStore) TopicPosts(ctx context.Context, v, tag string, p store.Page) ([]store.Post, error) {
	l, err := n.Store.TopicPosts(ctx, v, tag, p)
	return n.fixPosts(ctx, l), err
}
func (n *namedStore) Post(ctx context.Context, v, id string) (store.Post, error) {
	p, err := n.Store.Post(ctx, v, id)
	if err == nil {
		p = n.fixPosts(ctx, []store.Post{p})[0]
	}
	return p, err
}
func (n *namedStore) CreatePost(ctx context.Context, v, c string, np store.NewPost) (store.Post, error) {
	p, err := n.Store.CreatePost(ctx, v, c, np)
	if err == nil {
		p = n.fixPosts(ctx, []store.Post{p})[0]
	}
	return p, err
}
func (n *namedStore) Reply(ctx context.Context, v, id string, np store.NewPost) (store.Post, error) {
	p, err := n.Store.Reply(ctx, v, id, np)
	if err == nil {
		p = n.fixPosts(ctx, []store.Post{p})[0]
	}
	return p, err
}
func (n *namedStore) Profile(ctx context.Context, v, id string) (store.Profile, error) {
	p, err := n.Store.Profile(ctx, v, id)
	if err == nil {
		if full := n.book.Lookup(ctx, []string{p.ID})[p.ID]; full != "" {
			p.Name = full
		}
	}
	return p, err
}
func (n *namedStore) Relations(ctx context.Context, v, id, kind string, p store.Page) ([]store.Person, []store.Community, error) {
	people, coms, err := n.Store.Relations(ctx, v, id, kind, p)
	return n.fixPeople(ctx, people), coms, err
}
func (n *namedStore) Activity(ctx context.Context, v, scope string, p store.Page) ([]store.ActivityItem, error) {
	l, err := n.Store.Activity(ctx, v, scope, p)
	return n.fixActivity(ctx, l), err
}
func (n *namedStore) UserActivity(ctx context.Context, v, id string, p store.Page) ([]store.ActivityItem, error) {
	l, err := n.Store.UserActivity(ctx, v, id, p)
	return n.fixActivity(ctx, l), err
}
func (n *namedStore) fixActivity(ctx context.Context, items []store.ActivityItem) []store.ActivityItem {
	ids := make([]string, 0, len(items))
	for _, a := range items {
		ids = append(ids, a.Person.ID)
	}
	full := n.book.Lookup(ctx, ids)
	for i := range items {
		if v := full[items[i].Person.ID]; v != "" {
			items[i].Person.Name = v
		}
	}
	return items
}
func (n *namedStore) Notifications(ctx context.Context, v string, p store.Page) ([]store.Notification, int, error) {
	l, total, err := n.Store.Notifications(ctx, v, p)
	var ids []string
	for _, x := range l {
		for _, a := range x.Actors {
			ids = append(ids, a.ID)
		}
	}
	full := n.book.Lookup(ctx, ids)
	for i := range l {
		for j := range l[i].Actors {
			if v := full[l[i].Actors[j].ID]; v != "" {
				l[i].Actors[j].Name = v
			}
		}
	}
	return l, total, err
}
