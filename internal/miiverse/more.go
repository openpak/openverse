package miiverse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/openpak/openverse/internal/store"
)

// Yeahed: GET /posts?empathy_by={id}.
func (c *Client) Yeahed(ctx context.Context, viewer, id string, p store.Page) ([]store.Post, error) {
	var out wireList[wirePost]
	if err := c.do(ctx, http.MethodGet, "/posts"+q(p, url.Values{"empathy_by": {id}}), viewer, nil, &out); err != nil {
		return nil, err
	}
	return posts(out.Items, viewer), nil
}

// TopicPosts: GET /posts?topic_tag={tag}.
func (c *Client) TopicPosts(ctx context.Context, viewer, tag string, p store.Page) ([]store.Post, error) {
	var out wireList[wirePost]
	if err := c.do(ctx, http.MethodGet, "/posts"+q(p, url.Values{"topic_tag": {tag}}), viewer, nil, &out); err != nil {
		return nil, err
	}
	return posts(out.Items, viewer), nil
}

// UserActivity: GET /users/{id}/activity.
func (c *Client) UserActivity(ctx context.Context, viewer, id string, p store.Page) ([]store.ActivityItem, error) {
	var out struct {
		Items []wireActivity `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, "/users/"+url.PathEscape(id)+"/activity"+q(p, nil), viewer, nil, &out); err != nil {
		return nil, err
	}
	return activityItems(out.Items), nil
}

type wireActivity struct {
	ID        int64     `json:"id"`
	PID       int64     `json:"pid"`
	Name      string    `json:"name"`
	Category  string    `json:"category"`
	Subject   string    `json:"subject"`
	Link      string    `json:"link"`
	Image     string    `json:"image"`
	Count     int       `json:"count"`
	Digest    bool      `json:"digest"`
	CreatedAt time.Time `json:"createdAt"`
}

func activityItems(in []wireActivity) []store.ActivityItem {
	items := make([]store.ActivityItem, 0, len(in))
	for _, i := range in {
		items = append(items, store.ActivityItem{ID: strconv.FormatInt(i.ID, 10), Category: i.Category, Subject: i.Subject, Link: i.Link,
			Image: i.Image, Count: i.Count, Digest: i.Digest, Created: i.CreatedAt,
			Person: store.Person{ID: pidStr(i.PID), Name: i.Name, Avatar: avatarFor(i.PID, "")}})
	}
	return items
}

// DeleteActivity: DELETE /activity/{id}.
func (c *Client) DeleteActivity(ctx context.Context, viewer, id string) error {
	return c.do(ctx, http.MethodDelete, "/activity/"+url.PathEscape(id), viewer, nil, nil)
}

// Relations: GET /users/{id}/following (+ followed-communities), /followers or /friends.
func (c *Client) Relations(ctx context.Context, viewer, id, kind string, p store.Page) ([]store.Person, []store.Community, error) {
	switch kind {
	case "followers", "friends":
	default:
		kind = "following"
	}
	var out struct {
		Items []struct {
			PID  int64  `json:"pid"`
			Name string `json:"miiName"`
			PNID string `json:"pnid"`
		} `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, "/users/"+url.PathEscape(id)+"/"+kind+q(p, nil), viewer, nil, &out); err != nil {
		return nil, nil, err
	}
	people := make([]store.Person, 0, len(out.Items))
	for _, u := range out.Items {
		people = append(people, store.Person{ID: pidStr(u.PID), Name: u.Name, Handle: u.PNID, Avatar: avatarFor(u.PID, "")})
	}
	var coms []store.Community
	if kind == "following" {
		var cl wireList[wireCommunity]
		if err := c.do(ctx, http.MethodGet, "/users/"+url.PathEscape(id)+"/followed-communities"+q(p, nil), viewer, nil, &cl); err == nil {
			for _, w := range cl.Items {
				coms = append(coms, w.toStore())
			}
		}
	}
	return people, coms, nil
}

// SubCommunities: GET /communities?category=sub&parent_id={id}.
func (c *Client) SubCommunities(ctx context.Context, viewer, parent string) ([]store.Community, error) {
	var out wireList[wireCommunity]
	if err := c.do(ctx, http.MethodGet, "/communities"+q(store.Page{Limit: 90}, url.Values{"category": {"sub"}, "parent_id": {parent}}), viewer, nil, &out); err != nil {
		return nil, err
	}
	list := make([]store.Community, 0, len(out.Items))
	for _, w := range out.Items {
		list = append(list, w.toStore())
	}
	return list, nil
}

// SetSpoiler: POST /posts/{id}/edit/spoiler.
func (c *Client) SetSpoiler(ctx context.Context, viewer, post string, on bool) error {
	return c.do(ctx, http.MethodPost, "/posts/"+url.PathEscape(post)+"/edit/spoiler", viewer, map[string]any{"is_spoiler": on}, nil)
}

// Export: POST /self/export, the GDPR export.
func (c *Client) Export(ctx context.Context, viewer string) ([]byte, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodPost, "/self/export", viewer, map[string]any{}, &raw); err != nil {
		return nil, err
	}
	return json.MarshalIndent(raw, "", "  ")
}

// DeviceLinks: POST /device-links/list.
func (c *Client) DeviceLinks(ctx context.Context, account string) ([]store.DeviceLink, error) {
	var out struct {
		Items []struct {
			ID         string     `json:"id"`
			Class      string     `json:"deviceClass"`
			Label      string     `json:"label"`
			CreatedAt  time.Time  `json:"createdAt"`
			LastUsedAt *time.Time `json:"lastUsedAt"`
		} `json:"items"`
	}
	if err := c.do(ctx, http.MethodPost, "/device-links/list", "", map[string]any{"accountId": account}, &out); err != nil {
		return nil, err
	}
	list := make([]store.DeviceLink, 0, len(out.Items))
	for _, d := range out.Items {
		list = append(list, store.DeviceLink{ID: d.ID, Class: d.Class, Label: d.Label, Created: d.CreatedAt, LastUsed: d.LastUsedAt})
	}
	return list, nil
}

// RevokeDevice: POST /device-links/revoke.
func (c *Client) RevokeDevice(ctx context.Context, account, id string) error {
	return c.do(ctx, http.MethodPost, "/device-links/revoke", "", map[string]any{"accountId": account, "id": id}, nil)
}
