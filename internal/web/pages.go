package web

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/openpak/openverse/internal/store"
)

const bodyMax = 280

// PostView is a post as a card needs it.
type PostView struct {
	store.Post
	Signed bool // the visitor is signed in (Yeah and reply are offered)
	Mine   bool
	Detail bool // the post's own page: full text, more actions
	Back   string
}

func postViews(posts []store.Post, me *Me, detail bool, back string) []PostView {
	out := make([]PostView, 0, len(posts))
	for _, p := range posts {
		out = append(out, PostView{Post: p, Signed: me != nil, Mine: me != nil && me.ID == p.Author.ID, Detail: detail, Back: back})
	}
	return out
}

func (s *Site) meView(r *http.Request) *Me {
	if m, ok := s.me(r); ok {
		return &m
	}
	return nil
}

// ---- home, communities ------------------------------------------------------------------------

func (s *Site) home(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.me(r); ok {
		http.Redirect(w, r, "/feed", http.StatusSeeOther)
		return
	}
	s.communities(w, r)
}

type communitiesData struct {
	Communities []store.Community
	Total       int
	Welcome     bool // a visitor who is not signed in gets a short invitation above the grid
}

func (s *Site) communities(w http.ResponseWriter, r *http.Request) {
	list, total, err := s.store.Communities(r.Context(), store.Page{Limit: 120})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "communities", "Communities", communitiesData{Communities: list, Total: total, Welcome: s.meView(r) == nil})
}

type communityData struct {
	store.Community
	Items    []PostView
	Popular  bool
	CanPost  bool
	Pager    Pager
	Signed   bool
	ReadOnly bool
}

func (s *Site) community(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	me := s.meView(r)
	viewer := ""
	if me != nil {
		viewer = me.ID
	}
	com, err := s.store.Community(r.Context(), viewer, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	popular := r.URL.Query().Get("sort") == "popular"
	page := pageParam(r)
	posts, err := s.store.CommunityPosts(r.Context(), viewer, id, popular, pageFor(page))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	base := "/communities/" + url.PathEscape(id)
	if popular {
		base += "?sort=popular"
	}
	d := communityData{Community: com, Items: postViews(posts, me, false, r.URL.RequestURI()), Popular: popular, Signed: me != nil,
		CanPost: com.Open, Pager: pager(page, len(posts) == pageSize, base)}
	s.render(w, r, http.StatusOK, "community", com.Name, d)
}

func (s *Site) followCommunity(w http.ResponseWriter, r *http.Request) {
	me, ok := s.need(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if _, err := s.store.FollowCommunity(r.Context(), me.ID, id, r.FormValue("follow") == "1"); err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/communities/"+url.PathEscape(id), http.StatusSeeOther)
}

// ---- feeds ------------------------------------------------------------------------------------

type feedData struct {
	Scope string
	Posts []PostView
	Pager Pager
}

func (s *Site) feed(w http.ResponseWriter, r *http.Request) {
	me, ok := s.need(w, r)
	if !ok {
		return
	}
	scope := "following"
	if r.URL.Query().Get("scope") == "everyone" {
		scope = "everyone"
	}
	page := pageParam(r)
	posts, err := s.store.Feed(r.Context(), me.ID, scope, pageFor(page))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	base := "/feed?scope=" + scope
	s.render(w, r, http.StatusOK, "feed", "Feed", feedData{Scope: scope, Posts: postViews(posts, &me, false, r.URL.RequestURI()), Pager: pager(page, len(posts) == pageSize, base)})
}

type activityData struct {
	Scope string
	Items []store.ActivityItem
	Pager Pager
}

func (s *Site) activity(w http.ResponseWriter, r *http.Request) {
	me, ok := s.need(w, r)
	if !ok {
		return
	}
	scope := "everyone"
	if r.URL.Query().Get("scope") == "following" {
		scope = "following"
	}
	page := pageParam(r)
	items, err := s.store.Activity(r.Context(), me.ID, scope, pageFor(page))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "activity", "Activity", activityData{Scope: scope, Items: items, Pager: pager(page, len(items) == pageSize, "/activity?scope="+scope)})
}

// ---- notifications ------------------------------------------------------------------------------

type notificationsData struct {
	Items []store.Notification
	Pager Pager
}

func (s *Site) notifications(w http.ResponseWriter, r *http.Request) {
	me, ok := s.need(w, r)
	if !ok {
		return
	}
	page := pageParam(r)
	items, total, err := s.store.Notifications(r.Context(), me.ID, pageFor(page))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "notifications", "Notifications", notificationsData{Items: items, Pager: pager(page, page*pageSize < total, "/notifications")})
}

func (s *Site) notificationCount(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	n := 0
	if me, ok := s.me(r); ok {
		n, _ = s.store.UnreadNotifications(r.Context(), me.ID)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"count": n})
}

// ---- posting ------------------------------------------------------------------------------------

type composeData struct {
	Community *store.Community
	Choices   []store.Community // set when no community was picked yet
	ReadOnly  bool
	Error     string
	Body      string
	Spoiler   bool
	Max       int
}

func (s *Site) compose(w http.ResponseWriter, r *http.Request) {
	me, ok := s.need(w, r)
	if !ok {
		return
	}
	d := composeData{Max: bodyMax}
	if v, err := s.store.Viewer(r.Context(), me.ID); err == nil {
		d.ReadOnly = v.ReadOnly
	}
	if id := r.URL.Query().Get("community"); id != "" {
		com, err := s.store.Community(r.Context(), me.ID, id)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		d.Community = &com
	} else {
		list, _, err := s.store.Communities(r.Context(), store.Page{Limit: 120})
		if err != nil {
			s.fail(w, r, err)
			return
		}
		d.Choices = list
	}
	s.render(w, r, http.StatusOK, "compose", "New post", d)
}

// readPost validates what the composer or the reply box sent.
func readPost(r *http.Request) (store.NewPost, string) {
	n := store.NewPost{Body: strings.TrimSpace(strings.ReplaceAll(r.FormValue("body"), "\r\n", "\n")), Spoiler: r.FormValue("spoiler") == "1"}
	if utf8.RuneCountInString(n.Body) > bodyMax {
		return n, "That post is longer than 280 characters."
	}
	if d := strings.TrimSpace(r.FormValue("painting")); d != "" && r.FormValue("kind") == "painting" {
		raw, err := base64.StdEncoding.DecodeString(d)
		if err != nil || len(raw) < 62 || len(raw) > 64<<10 || raw[0] != 'B' || raw[1] != 'M' {
			return n, "That drawing could not be read. Draw it again."
		}
		n.Drawing = d
	}
	if n.Body == "" && n.Drawing == "" {
		return n, "Write something or draw something first."
	}
	return n, ""
}

func (s *Site) createPost(w http.ResponseWriter, r *http.Request) {
	me, ok := s.need(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	id := r.FormValue("community")
	com, err := s.store.Community(r.Context(), me.ID, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	n, msg := readPost(r)
	if msg == "" {
		var p store.Post
		if p, err = s.store.CreatePost(r.Context(), me.ID, id, n); err == nil {
			http.Redirect(w, r, "/posts/"+url.PathEscape(p.ID), http.StatusSeeOther)
			return
		}
		msg = s.postError(err)
		if msg == "" {
			s.fail(w, r, err)
			return
		}
	}
	d := composeData{Community: &com, Error: msg, Body: n.Body, Spoiler: n.Spoiler, Max: bodyMax}
	s.render(w, r, http.StatusUnprocessableEntity, "compose", "New post", d)
}

// postError turns a refusal into words for the person who posted; "" means it was not a refusal.
func (s *Site) postError(err error) string {
	switch {
	case errors.Is(err, store.ErrRejected):
		return "OpenVerse could not accept that post. It may contain something the community rules do not allow."
	case errors.Is(err, store.ErrForbidden):
		return "This account cannot post here. If it is supervised, a parent or guardian controls that."
	case errors.Is(err, store.ErrRateLimit):
		return "You are posting a little fast. Wait a moment and try again."
	}
	return ""
}

// ---- a post -------------------------------------------------------------------------------------

type postData struct {
	Post       PostView
	Replies    []PostView
	Newest     bool
	CanReply   bool
	Error      string
	ReplyDraft string
	Max        int
}

func (s *Site) post(w http.ResponseWriter, r *http.Request) {
	s.showPost(w, r, http.StatusOK, "", "")
}

func (s *Site) showPost(w http.ResponseWriter, r *http.Request, status int, msg, draft string) {
	id := r.PathValue("id")
	me := s.meView(r)
	viewer := ""
	if me != nil {
		viewer = me.ID
	}
	p, err := s.store.Post(r.Context(), viewer, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if p.ParentID != "" { // a reply's own page is its thread
		http.Redirect(w, r, "/posts/"+url.PathEscape(p.ParentID)+"#post-"+url.PathEscape(p.ID), http.StatusSeeOther)
		return
	}
	newest := r.URL.Query().Get("sort") == "newest"
	replies, err := s.store.Replies(r.Context(), viewer, id, newest)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	back := "/posts/" + url.PathEscape(id)
	d := postData{Post: postViews([]store.Post{p}, me, true, back)[0], Replies: postViews(replies, me, true, back), Newest: newest,
		CanReply: me != nil && !p.Removed, Error: msg, ReplyDraft: draft, Max: bodyMax}
	title := p.Author.Name + "'s post"
	s.render(w, r, status, "post", title, d)
}

func (s *Site) reply(w http.ResponseWriter, r *http.Request) {
	me, ok := s.need(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
		return
	}
	id := r.PathValue("id")
	n, msg := readPost(r)
	if msg == "" {
		p, err := s.store.Reply(r.Context(), me.ID, id, n)
		if err == nil {
			http.Redirect(w, r, "/posts/"+url.PathEscape(id)+"#post-"+url.PathEscape(p.ID), http.StatusSeeOther)
			return
		}
		if msg = s.postError(err); msg == "" {
			s.fail(w, r, err)
			return
		}
	}
	s.showPost(w, r, http.StatusUnprocessableEntity, msg, n.Body)
}

func (s *Site) yeah(w http.ResponseWriter, r *http.Request) {
	wantsJSON := strings.Contains(r.Header.Get("Accept"), "application/json")
	me, ok := s.me(r)
	if !ok {
		if wantsJSON {
			http.Error(w, `{"error":"sign in"}`, http.StatusUnauthorized)
			return
		}
		s.need(w, r)
		return
	}
	id := r.PathValue("id")
	n, err := s.store.Yeah(r.Context(), me.ID, id, r.FormValue("add") == "1")
	if wantsJSON {
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"failed"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]int{"count": n})
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, backTo(r, "/posts/"+url.PathEscape(id)), http.StatusSeeOther)
}

// backTo is the page a form came from, if it is one of ours.
func backTo(r *http.Request, fallback string) string {
	if b := r.FormValue("back"); b != "" {
		return localPath(b)
	}
	return fallback
}

func (s *Site) deletePost(w http.ResponseWriter, r *http.Request) {
	me, ok := s.need(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	p, err := s.store.Post(r.Context(), me.ID, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.store.DeletePost(r.Context(), me.ID, id); err != nil {
		s.fail(w, r, err)
		return
	}
	if p.ParentID != "" {
		http.Redirect(w, r, "/posts/"+url.PathEscape(p.ParentID), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/me", http.StatusSeeOther)
}

func (s *Site) report(w http.ResponseWriter, r *http.Request) {
	me, ok := s.need(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	reason, _ := strconv.Atoi(r.FormValue("reason"))
	msg := strings.TrimSpace(r.FormValue("message"))
	if len(msg) > 280 {
		msg = msg[:280]
	}
	if err := s.store.ReportPost(r.Context(), me.ID, id, reason, msg); err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/posts/"+url.PathEscape(id)+"?reported=1", http.StatusSeeOther)
}

// ---- people -------------------------------------------------------------------------------------

func (s *Site) meRedirect(w http.ResponseWriter, r *http.Request) {
	me, ok := s.need(w, r)
	if !ok {
		return
	}
	http.Redirect(w, r, "/users/"+url.PathEscape(me.ID), http.StatusSeeOther)
}

type profileData struct {
	store.Profile
	Items  []PostView
	Pager  Pager
	Signed bool
}

func (s *Site) profile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	me := s.meView(r)
	viewer := ""
	if me != nil {
		viewer = me.ID
	}
	pr, err := s.store.Profile(r.Context(), viewer, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	page := pageParam(r)
	posts, err := s.store.UserPosts(r.Context(), viewer, id, pageFor(page))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	d := profileData{Profile: pr, Items: postViews(posts, me, false, r.URL.RequestURI()), Signed: me != nil,
		Pager: pager(page, len(posts) == pageSize, "/users/"+url.PathEscape(id))}
	s.render(w, r, http.StatusOK, "profile", pr.Name, d)
}

func (s *Site) followUser(w http.ResponseWriter, r *http.Request) {
	me, ok := s.need(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if _, err := s.store.FollowUser(r.Context(), me.ID, id, r.FormValue("follow") == "1"); err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/users/"+url.PathEscape(id), http.StatusSeeOther)
}

// ---- settings -----------------------------------------------------------------------------------

type settingsData struct {
	store.Settings
	Saved bool
	Name  string
}

func (s *Site) settings(w http.ResponseWriter, r *http.Request) {
	me, ok := s.need(w, r)
	if !ok {
		return
	}
	st, err := s.store.Settings(r.Context(), me.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "settings", "Settings", settingsData{Settings: st, Saved: r.URL.Query().Get("saved") == "1", Name: me.Name})
}

func (s *Site) saveSettings(w http.ResponseWriter, r *http.Request) {
	me, ok := s.need(w, r)
	if !ok {
		return
	}
	cur, err := s.store.Settings(r.Context(), me.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	next := cur
	next.ActivityOn = r.FormValue("activity") == "1"
	chosen := map[string]bool{}
	for _, c := range r.Form["category"] {
		chosen[c] = true
	}
	next.ActivityCategories = nil
	for _, c := range cur.ActivityCategories {
		next.ActivityCategories = append(next.ActivityCategories, store.ActivityCategory{ID: c.ID, Enabled: chosen[c.ID]})
	}
	next.ProfileVisibility = "public"
	if r.FormValue("visibility") == "users_only" {
		next.ProfileVisibility = "users_only"
	}
	next.ShowCountry = r.FormValue("country") == "1"
	next.NotifyYeah = r.FormValue("notify_yeah") == "1"
	next.NotifyFollow = r.FormValue("notify_follow") == "1"
	next.NotifyReply = r.FormValue("notify_reply") == "1"
	next.Comment = strings.TrimSpace(r.FormValue("comment"))
	if utf8.RuneCountInString(next.Comment) > 200 {
		next.Comment = string([]rune(next.Comment)[:200])
	}
	if err := s.store.SaveSettings(r.Context(), me.ID, next); err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/settings?saved=1", http.StatusSeeOther)
}

// ---- welcome, about -----------------------------------------------------------------------------

func (s *Site) welcome(w http.ResponseWriter, r *http.Request) {
	me, ok := s.me(r)
	if !ok {
		http.Redirect(w, r, "/auth/login?next=/welcome", http.StatusSeeOther)
		return
	}
	next := localPath(r.URL.Query().Get("next"))
	if me.Rules {
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}
	s.render(w, r, http.StatusOK, "welcome", "Welcome", map[string]string{"Next": next, "Name": me.Name})
}

func (s *Site) acceptWelcome(w http.ResponseWriter, r *http.Request) {
	me, ok := s.me(r)
	if !ok {
		http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
		return
	}
	if err := s.store.AcceptRules(r.Context(), me.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	me.Rules = true
	s.setSession(w, me)
	http.Redirect(w, r, localPath(r.FormValue("next")), http.StatusSeeOther)
}

func (s *Site) about(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusOK, "about", "About", nil)
}
