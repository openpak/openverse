package miiverse

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/openpak/openverse/internal/store"
)

// ---- identity -------------------------------------------------------------------------------

// Resolve: POST /sso/resolve. Finds or creates the OpenVerse person for an OpenPak account.
func (c *Client) Resolve(ctx context.Context, account, name string, wiiuPID *int64) (store.Identity, error) {
	var out struct {
		PID           int64 `json:"pid"`
		RulesAccepted bool  `json:"rulesAccepted"`
	}
	body := map[string]any{"accountId": account, "displayName": name, "wiiuPid": wiiuPID}
	if err := c.do(ctx, http.MethodPost, "/sso/resolve", "", body, &out); err != nil {
		return store.Identity{}, err
	}
	return store.Identity{Account: account, Name: name, ID: pidStr(out.PID), RulesAccepted: out.RulesAccepted}, nil
}

// AcceptRules: POST /sso/accept-rules.
func (c *Client) AcceptRules(ctx context.Context, viewer string) error {
	return c.do(ctx, http.MethodPost, "/sso/accept-rules", viewer, map[string]any{}, nil)
}

// Viewer: GET /self.
func (c *Client) Viewer(ctx context.Context, viewer string) (store.Viewer, error) {
	var s struct {
		PID      int64 `json:"pid"`
		ReadOnly bool  `json:"readOnly"`
		Off      bool  `json:"openverseOff"`
		Content  struct {
			Communities []string `json:"followed_communities"`
			Users       []int64  `json:"followed_users"`
		} `json:"content"`
		Permissions struct {
			Moderator bool `json:"moderator"`
		} `json:"permissions"`
	}
	if err := c.getOne(ctx, "/self", viewer, &s); err != nil {
		return store.Viewer{}, err
	}
	v := store.Viewer{ID: pidStr(s.PID), ReadOnly: s.ReadOnly || s.Off, Off: s.Off, Moderator: s.Permissions.Moderator,
		FollowedCommunities: map[string]bool{}, FollowedUsers: map[string]bool{}}
	for _, id := range s.Content.Communities {
		v.FollowedCommunities[id] = true
	}
	for _, id := range s.Content.Users {
		v.FollowedUsers[pidStr(id)] = true
	}
	return v, nil
}

// UnreadNotifications: GET /self/notifications.
func (c *Client) UnreadNotifications(ctx context.Context, viewer string) (int, error) {
	var out struct {
		Unread int `json:"unreadNotifications"`
	}
	if err := c.getOne(ctx, "/self/notifications", viewer, &out); err != nil {
		return 0, err
	}
	return out.Unread, nil
}

// ---- communities ----------------------------------------------------------------------------

// Communities: GET /communities?category=listed.
func (c *Client) Communities(ctx context.Context, p store.Page) ([]store.Community, int, error) {
	var out wireList[wireCommunity]
	if err := c.do(ctx, http.MethodGet, "/communities"+q(p, url.Values{"category": {"listed"}}), "", nil, &out); err != nil {
		return nil, 0, err
	}
	list := make([]store.Community, 0, len(out.Items))
	for _, w := range out.Items {
		list = append(list, w.toStore())
	}
	return list, out.Total, nil
}

// Community: GET /communities/{id} (+ /stats for the post count, + sub-communities for "related").
func (c *Client) Community(ctx context.Context, viewer, id string) (store.Community, error) {
	var w wireCommunity
	if err := c.getOne(ctx, "/communities/"+url.PathEscape(id), viewer, &w); err != nil {
		return store.Community{}, err
	}
	com := w.toStore()
	var st struct {
		TotalPosts int `json:"totalPosts"`
	}
	if err := c.getOne(ctx, "/communities/"+url.PathEscape(id)+"/stats", viewer, &st); err == nil {
		com.Posts = st.TotalPosts
	}
	if viewer != "" {
		if v, err := c.Viewer(ctx, viewer); err == nil {
			com.Following = v.FollowedCommunities[id]
		}
	}
	return com, nil
}

// CommunityPosts: GET /communities/{id}/feed/fresh or /feed/popular.
func (c *Client) CommunityPosts(ctx context.Context, viewer, id string, popular bool, p store.Page) ([]store.Post, error) {
	kind := "fresh"
	if popular {
		kind = "popular"
	}
	var out wireList[wirePost]
	if err := c.do(ctx, http.MethodGet, "/communities/"+url.PathEscape(id)+"/feed/"+kind+q(p, nil), viewer, nil, &out); err != nil {
		return nil, err
	}
	return posts(out.Items, viewer), nil
}

// FollowCommunity: POST or DELETE /communities/{id}/followers/@me.
func (c *Client) FollowCommunity(ctx context.Context, viewer, id string, follow bool) (int, error) {
	return c.follow(ctx, viewer, "/communities/"+url.PathEscape(id)+"/followers/@me", follow)
}

// FollowUser: POST or DELETE /users/{id}/followers/@me.
func (c *Client) FollowUser(ctx context.Context, viewer, id string, follow bool) (int, error) {
	return c.follow(ctx, viewer, "/users/"+url.PathEscape(id)+"/followers/@me", follow)
}

func (c *Client) follow(ctx context.Context, viewer, path string, follow bool) (int, error) {
	m := http.MethodDelete
	if follow {
		m = http.MethodPost
	}
	var out struct {
		Count int `json:"follower_count"`
	}
	err := c.do(ctx, m, path, viewer, nil, &out)
	return out.Count, err
}

func posts(in []wirePost, viewer string) []store.Post {
	out := make([]store.Post, 0, len(in))
	for _, w := range in {
		out = append(out, w.toStore(viewer))
	}
	return out
}

// ---- feeds ----------------------------------------------------------------------------------

// Feed: GET /activity-feed/following (people and communities you follow) or /fresh (everyone).
func (c *Client) Feed(ctx context.Context, viewer, scope string, p store.Page) ([]store.Post, error) {
	kind := "following"
	if scope == "everyone" {
		kind = "fresh"
	}
	var out wireList[wirePost]
	if err := c.do(ctx, http.MethodGet, "/activity-feed/"+kind+q(p, nil), viewer, nil, &out); err != nil {
		return nil, err
	}
	return posts(out.Items, viewer), nil
}

// Activity: GET /activity?scope=everyone|following.
func (c *Client) Activity(ctx context.Context, viewer, scope string, p store.Page) ([]store.ActivityItem, error) {
	var out struct {
		Items []wireActivity `json:"items"`
	}
	if scope != "following" {
		scope = "everyone"
	}
	if err := c.do(ctx, http.MethodGet, "/activity"+q(p, url.Values{"scope": {scope}}), viewer, nil, &out); err != nil {
		return nil, err
	}
	return activityItems(out.Items), nil
}

// ---- posts ----------------------------------------------------------------------------------

// Post: GET /posts/{id}.
func (c *Client) Post(ctx context.Context, viewer, id string) (store.Post, error) {
	var w wirePost
	if err := c.getOne(ctx, "/posts/"+url.PathEscape(id), viewer, &w); err != nil {
		return store.Post{}, err
	}
	return w.toStore(viewer), nil
}

// Replies: GET /posts?parent_id={id}&include_replies=true&sort=oldest|newest.
func (c *Client) Replies(ctx context.Context, viewer, id string, newestFirst bool) ([]store.Post, error) {
	sort := "oldest"
	if newestFirst {
		sort = "newest"
	}
	var out wireList[wirePost]
	err := c.do(ctx, http.MethodGet, "/posts"+q(store.Page{Limit: 200}, url.Values{"parent_id": {id}, "include_replies": {"true"}, "sort": {sort}}), viewer, nil, &out)
	if err != nil {
		return nil, err
	}
	return posts(out.Items, viewer), nil
}

func newPostBody(n store.NewPost) map[string]any {
	b := map[string]any{"body": n.Body, "feelingId": 0, "isSpoiler": n.Spoiler, "isAppJumpable": false}
	if n.Drawing != "" {
		b["painting"] = map[string]any{"file": n.Drawing, "isBmp": true}
	}
	return b
}

// CreatePost: POST /communities/{id}/posts.
func (c *Client) CreatePost(ctx context.Context, viewer, community string, n store.NewPost) (store.Post, error) {
	var w wirePost
	if err := c.do(ctx, http.MethodPost, "/communities/"+url.PathEscape(community)+"/posts", viewer, newPostBody(n), &w); err != nil {
		return store.Post{}, err
	}
	return w.toStore(viewer), nil
}

// Reply: POST /posts/{id}/replies.
func (c *Client) Reply(ctx context.Context, viewer, post string, n store.NewPost) (store.Post, error) {
	var w wirePost
	if err := c.do(ctx, http.MethodPost, "/posts/"+url.PathEscape(post)+"/replies", viewer, newPostBody(n), &w); err != nil {
		return store.Post{}, err
	}
	return w.toStore(viewer), nil
}

// Yeah: POST /posts/{id}/empathies {action: add|remove}.
func (c *Client) Yeah(ctx context.Context, viewer, post string, add bool) (int, error) {
	action := "remove"
	if add {
		action = "add"
	}
	var out struct {
		Count int `json:"empathy_count"`
	}
	err := c.do(ctx, http.MethodPost, "/posts/"+url.PathEscape(post)+"/empathies", viewer, map[string]any{"action": action}, &out)
	return out.Count, err
}

// DeletePost: DELETE /posts/{id}.
func (c *Client) DeletePost(ctx context.Context, viewer, post string) error {
	return c.do(ctx, http.MethodDelete, "/posts/"+url.PathEscape(post), viewer, nil, nil)
}

// ReportPost: POST /posts/{id}/report.
func (c *Client) ReportPost(ctx context.Context, viewer, post string, reason int, message string) error {
	return c.do(ctx, http.MethodPost, "/posts/"+url.PathEscape(post)+"/report", viewer, map[string]any{"reasonId": reason, "message": message}, nil)
}

// ---- people ---------------------------------------------------------------------------------

// Profile: GET /users/{id}/profile.
func (c *Client) Profile(ctx context.Context, viewer, id string) (store.Profile, error) {
	var w struct {
		PID       int64    `json:"pid"`
		Name      string   `json:"miiName"`
		Username  string   `json:"username"`
		Flags     []string `json:"flags"`
		Followers int      `json:"followers"`
		Posts     int      `json:"posts"`
		Info      struct {
			Country *string `json:"country"`
			Comment *string `json:"comment"`
		} `json:"profileInfo"`
	}
	if err := c.getOne(ctx, "/users/"+url.PathEscape(id)+"/profile", viewer, &w); err != nil {
		return store.Profile{}, err
	}
	p := store.Profile{Followers: w.Followers, Posts: w.Posts, Flags: w.Flags,
		Person: store.Person{ID: pidStr(w.PID), Name: w.Name, Handle: w.Username, Avatar: avatarFor(w.PID, "")}}
	for _, f := range w.Flags {
		if f == "verified" {
			p.Verified = true
		}
	}
	if w.Info.Country != nil {
		p.Country = *w.Info.Country
	}
	if w.Info.Comment != nil {
		p.Bio = *w.Info.Comment
	}
	p.Self = viewer != "" && viewer == p.ID
	if viewer != "" && !p.Self {
		if v, err := c.Viewer(ctx, viewer); err == nil {
			p.Following = v.FollowedUsers[p.ID]
		}
	}
	return p, nil
}

// UserPosts: GET /users/{id}/posts.
func (c *Client) UserPosts(ctx context.Context, viewer, id string, p store.Page) ([]store.Post, error) {
	var out wireList[wirePost]
	if err := c.do(ctx, http.MethodGet, "/users/"+url.PathEscape(id)+"/posts"+q(p, url.Values{"type": {"posts"}}), viewer, nil, &out); err != nil {
		return nil, err
	}
	return posts(out.Items, viewer), nil
}

// ---- notifications and settings -------------------------------------------------------------

// Notifications: GET /notifications?markAsRead=true.
func (c *Client) Notifications(ctx context.Context, viewer string, p store.Page) ([]store.Notification, int, error) {
	type shallow struct {
		PID  int64  `json:"pid"`
		Name string `json:"miiName"`
	}
	var out struct {
		Items []struct {
			HasRead   bool      `json:"hasRead"`
			UpdatedAt time.Time `json:"updatedAt"`
			Notif     struct {
				Type    string `json:"type"`
				Content struct {
					Users []struct {
						User shallow `json:"user"`
					} `json:"users"`
					PostID string  `json:"postId"`
					Parent string  `json:"parent"`
					User   shallow `json:"user"`
					Text   string  `json:"text"`
					Link   string  `json:"link"`
					Reason string  `json:"reason"`
				} `json:"content"`
			} `json:"notif"`
		} `json:"items"`
		Total int `json:"total"`
	}
	if err := c.do(ctx, http.MethodGet, "/notifications"+q(p, url.Values{"markAsRead": {"true"}}), viewer, nil, &out); err != nil {
		return nil, 0, err
	}
	person := func(s shallow) store.Person {
		return store.Person{ID: pidStr(s.PID), Name: s.Name, Avatar: avatarFor(s.PID, "")}
	}
	var list []store.Notification
	for _, i := range out.Items {
		n := store.Notification{Created: i.UpdatedAt, Read: i.HasRead}
		ct := i.Notif.Content
		switch i.Notif.Type {
		case "follow", "empathy":
			n.Kind = "follow"
			if i.Notif.Type == "empathy" {
				n.Kind, n.PostID = "yeah", ct.PostID
			}
			for k, u := range ct.Users {
				if k < 2 {
					n.Actors = append(n.Actors, person(u.User))
				}
			}
			if len(ct.Users) > 2 {
				n.Others = len(ct.Users) - 2
			}
		case "reply":
			n.Kind, n.PostID = "reply", ct.Parent
			n.Actors = []store.Person{person(ct.User)}
		case "system":
			n.Kind, n.Text, n.Link = "system", ct.Text, ct.Link
		case "postDeleted":
			n.Kind, n.Text = "removed", ct.Reason
		case "limitedFromPosting":
			n.Kind, n.Text = "limited", ct.Reason
		default:
			continue
		}
		list = append(list, n)
	}
	return list, out.Total, nil
}

// Settings: GET /activity/settings and GET /users/@me/settings.
func (c *Client) Settings(ctx context.Context, viewer string) (store.Settings, error) {
	var a struct {
		Enabled    bool `json:"enabled"`
		Categories []struct {
			ID      string `json:"id"`
			Enabled bool   `json:"enabled"`
		} `json:"categories"`
	}
	var u struct {
		Visibility   string  `json:"profileVisibility"`
		Country      bool    `json:"countryVisible"`
		Birthday     bool    `json:"birthdayVisible"`
		GameSkill    bool    `json:"gameSkillVisible"`
		Comment      *string `json:"comment"`
		NotifyYeah   bool    `json:"notifyEmpathy"`
		NotifyFollow bool    `json:"notifyFollows"`
		NotifyReply  bool    `json:"notifyReply"`
	}
	s := store.Settings{}
	if err := c.getOne(ctx, "/users/@me/settings", viewer, &u); err != nil {
		return s, err
	}
	s.ProfileVisibility, s.ShowCountry, s.NotifyYeah, s.NotifyFollow, s.NotifyReply = u.Visibility, u.Country, u.NotifyYeah, u.NotifyFollow, u.NotifyReply
	s.ShowBirthday, s.ShowGameSkill = u.Birthday, u.GameSkill
	if u.Comment != nil {
		s.Comment = *u.Comment
	}
	if err := c.getOne(ctx, "/activity/settings", viewer, &a); err == nil {
		s.ActivityOn = a.Enabled
		for _, cat := range a.Categories {
			s.ActivityCategories = append(s.ActivityCategories, store.ActivityCategory{ID: cat.ID, Enabled: cat.Enabled})
		}
	}
	return s, nil
}

// SaveSettings: POST /users/@me/settings and POST /activity/settings. The user settings call is
// a full replace, so the fields this page does not edit are read first and sent back unchanged.
func (c *Client) SaveSettings(ctx context.Context, viewer string, s store.Settings) error {
	var comment any
	if t := strings.TrimSpace(s.Comment); t != "" {
		comment = t
	}
	body := map[string]any{"profileVisibility": s.ProfileVisibility, "countryVisible": s.ShowCountry, "birthdayVisible": s.ShowBirthday,
		"gameSkillVisible": s.ShowGameSkill, "comment": comment, "notifyEmpathy": s.NotifyYeah, "notifyFollows": s.NotifyFollow, "notifyReply": s.NotifyReply}
	if err := c.do(ctx, http.MethodPost, "/users/@me/settings", viewer, body, nil); err != nil {
		return err
	}
	var cats []string
	for _, cat := range s.ActivityCategories {
		if cat.Enabled {
			cats = append(cats, cat.ID)
		}
	}
	if cats == nil {
		cats = []string{}
	}
	return c.do(ctx, http.MethodPost, "/activity/settings", viewer, map[string]any{"enabled": s.ActivityOn, "categories": cats}, nil)
}
