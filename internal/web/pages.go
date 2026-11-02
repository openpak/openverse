package web

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/openpak/openverse/internal/store"
)

const bodyMax = 280

// PostView is a post as a card needs it.
type PostView struct {
	store.Post
	Signed   bool // the visitor is signed in (Yeah and reply are offered)
	ReadOnly bool // a supervised account that may read but not act
	Mine     bool
	Detail   bool // the post's own page: full text, more actions
	Back     string
}

func postViews(posts []store.Post, me *Me, ro, detail bool, back string) []PostView {
	out := make([]PostView, 0, len(posts))
	for _, p := range posts {
		out = append(out, PostView{Post: p, Signed: me != nil, ReadOnly: ro, Mine: me != nil && me.ID == p.Author.ID, Detail: detail, Back: back})
	}
	return out
}

// readOnly is whether the signed-in visitor (nil = a guest) may not act.
func (s *Site) readOnly(r *http.Request, me *Me) bool {
	return me != nil && s.access(r.Context(), *me).ReadOnly
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

// browseData is the community browser: pinned rooms, console pills, a search field and the games.
// /communities and the community picker of the composer share it.
type browseData struct {
	Base      string // /communities or /new
	Link      string // tile link pattern, %s is the community id
	Query     string
	Console   string
	Consoles  []string
	Popular   []store.Community // only on /communities, unfiltered
	Yours     []store.Community // signed in, unfiltered
	Rooms     []store.Community
	Games     []store.Community
	Matches   int // games after the filters
	Total     int // all communities
	Welcome   bool
	More      string // "Show more" address, "" when everything is shown
	Filtering bool
}

const browseStep = 30

var consoleOrder = []string{"Switch", "Wii U", "3DS", "Wii", "DS", "PS3", "Xbox 360", "Xbox", "PS2", "PSP", "Vita"}

func (s *Site) browse(r *http.Request, base, link string, top bool) (browseData, error) {
	list, total, err := s.store.Communities(r.Context(), store.Page{Limit: 600})
	if err != nil {
		return browseData{}, err
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(q) > 60 {
		q = string([]rune(q)[:60])
	}
	d := browseData{Base: base, Link: link, Query: q, Total: total, Welcome: s.meView(r) == nil, Filtering: false}
	have := map[string]bool{}
	for _, c := range list {
		for _, k := range c.Consoles {
			have[k] = true
		}
	}
	for _, k := range consoleOrder {
		if have[k] {
			d.Consoles = append(d.Consoles, k)
			delete(have, k)
		}
	}
	var rest []string
	for k := range have {
		rest = append(rest, k)
	}
	sort.Strings(rest)
	d.Consoles = append(d.Consoles, rest...)
	if c := r.URL.Query().Get("console"); c != "" {
		for _, k := range d.Consoles {
			if k == c {
				d.Console = c
			}
		}
	}
	d.Filtering = d.Console != "" || q != ""
	match := func(c store.Community) bool {
		return q == "" || strings.Contains(strings.ToLower(c.Name), strings.ToLower(q))
	}
	var games []store.Community
	for _, c := range list {
		switch {
		case c.General:
			if d.Console == "" && match(c) {
				d.Rooms = append(d.Rooms, c)
			}
		case match(c) && (d.Console == "" || hasConsole(c, d.Console)):
			games = append(games, c)
		}
	}
	d.Matches = len(games)
	show := browseStep
	if n, err := strconv.Atoi(r.URL.Query().Get("show")); err == nil && n > browseStep && n <= 1000 {
		show = n
	}
	if show < len(games) {
		more := url.Values{}
		if d.Console != "" {
			more.Set("console", d.Console)
		}
		if q != "" {
			more.Set("q", q)
		}
		more.Set("show", strconv.Itoa(show+browseStep))
		d.More = base + "?" + more.Encode() + "#games"
		games = games[:show]
	}
	d.Games = games
	if top && !d.Filtering {
		viewer := ""
		if me := s.meView(r); me != nil {
			viewer = me.ID
		}
		if pop, err := s.store.Popular(r.Context(), viewer, 10); err == nil {
			d.Popular = pop
		}
		if me := s.meView(r); me != nil {
			if v := s.access(r.Context(), *me); len(v.FollowedCommunities) > 0 {
				for _, c := range list {
					if v.FollowedCommunities[c.ID] {
						d.Yours = append(d.Yours, c)
					}
				}
			}
		}
	}
	return d, nil
}

func hasConsole(c store.Community, k string) bool {
	for _, x := range c.Consoles {
		if x == k {
			return true
		}
	}
	return false
}

func (s *Site) communities(w http.ResponseWriter, r *http.Request) {
	d, err := s.browse(r, "/communities", "/communities/%s", true)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.renderKey(w, r, http.StatusOK, "communities", "communities.title", d)
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
	ro := s.readOnly(r, me)
	if subs, err := s.store.SubCommunities(r.Context(), viewer, id); err == nil && len(subs) > 0 {
		com.HasRelated = true
	}
	d := communityData{Community: com, Items: postViews(posts, me, ro, false, r.URL.RequestURI()), Popular: popular, Signed: me != nil,
		CanPost: com.Open && !ro, ReadOnly: ro, Pager: pager(page, len(posts) == pageSize, base)}
	s.render(w, r, http.StatusOK, "community", com.Name, d)
}

func (s *Site) followCommunity(w http.ResponseWriter, r *http.Request) {
	me, ok := s.canWrite(w, r)
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
	s.renderKey(w, r, http.StatusOK, "feed", "nav.feed", feedData{Scope: scope, Posts: postViews(posts, &me, s.readOnly(r, &me), false, r.URL.RequestURI()), Pager: pager(page, len(posts) == pageSize, base)})
}

// ActivityRow is an activity post with whether the viewer may remove it.
type ActivityRow struct {
	store.ActivityItem
	Removable bool
}

type activityData struct {
	Scope string
	Items []ActivityRow
	Back  string
	Pager Pager
}

// activityRows drops categories this build has no sentence for (rather than show them half-made)
// and marks the ones the viewer may remove: their own, or any for a moderator.
func (s *Site) activityRows(r *http.Request, me Me, items []store.ActivityItem) []ActivityRow {
	tr := s.tr(r)
	mod := s.access(r.Context(), me).Moderator
	rows := make([]ActivityRow, 0, len(items))
	for _, a := range items {
		if !a.Digest && tr.Str("activity.text."+a.Category) == "activity.text."+a.Category {
			continue
		}
		rows = append(rows, ActivityRow{ActivityItem: a, Removable: mod || a.Person.ID == me.ID})
	}
	return rows
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
	base := "/activity?scope=" + scope
	if scope == "everyone" {
		base = "/activity"
	}
	s.renderKey(w, r, http.StatusOK, "activity", "nav.activity", activityData{Scope: scope, Items: s.activityRows(r, me, items), Back: base,
		Pager: pager(page, len(items) == pageSize, "/activity?scope="+scope)})
}

func (s *Site) deleteActivity(w http.ResponseWriter, r *http.Request) {
	me, ok := s.canWrite(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteActivity(r.Context(), me.ID, r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	back := r.FormValue("back")
	if !strings.HasPrefix(back, "/activity") && !strings.HasPrefix(back, "/users/") {
		back = "/activity"
	}
	http.Redirect(w, r, localPath(back), http.StatusSeeOther)
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
	s.renderKey(w, r, http.StatusOK, "notifications", "nav.notifications", notificationsData{Items: items, Pager: pager(page, page*pageSize < total, "/notifications")})
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
	Browse    *browseData // set when no community was picked yet
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
	d := composeData{Max: bodyMax, ReadOnly: s.access(r.Context(), me).ReadOnly}
	if id := r.URL.Query().Get("community"); id != "" {
		com, err := s.store.Community(r.Context(), me.ID, id)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		d.Community = &com
	} else {
		bd, err := s.browse(r, "/new", "/new?community=%s", false)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		d.Browse = &bd
	}
	s.renderKey(w, r, http.StatusOK, "compose", "nav.newpost", d)
}

// readPost validates what the composer or the reply box sent.
func (s *Site) readPost(r *http.Request) (store.NewPost, string) {
	tr := s.tr(r)
	n := store.NewPost{Body: strings.TrimSpace(strings.ReplaceAll(r.FormValue("body"), "\r\n", "\n")), Spoiler: r.FormValue("spoiler") == "1"}
	if utf8.RuneCountInString(n.Body) > bodyMax {
		return n, tr.Str("post.err_long")
	}
	if d := strings.TrimSpace(r.FormValue("painting")); d != "" && r.FormValue("kind") == "painting" {
		raw, err := base64.StdEncoding.DecodeString(d)
		if err != nil || len(raw) < 62 || len(raw) > 64<<10 || raw[0] != 'B' || raw[1] != 'M' {
			return n, tr.Str("post.err_drawing")
		}
		n.Drawing = d
	}
	if n.Body == "" && n.Drawing == "" {
		return n, tr.Str("post.err_empty")
	}
	return n, ""
}

func (s *Site) createPost(w http.ResponseWriter, r *http.Request) {
	me, ok := s.canWrite(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, s.tr(r).Str("err.big"), http.StatusRequestEntityTooLarge)
		return
	}
	id := r.FormValue("community")
	com, err := s.store.Community(r.Context(), me.ID, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	n, msg := s.readPost(r)
	if msg == "" {
		var p store.Post
		if p, err = s.store.CreatePost(r.Context(), me.ID, id, n); err == nil {
			http.Redirect(w, r, "/posts/"+url.PathEscape(p.ID), http.StatusSeeOther)
			return
		}
		msg = s.postError(r, err)
		if msg == "" {
			s.fail(w, r, err)
			return
		}
	}
	d := composeData{Community: &com, Error: msg, Body: n.Body, Spoiler: n.Spoiler, Max: bodyMax}
	s.renderKey(w, r, http.StatusUnprocessableEntity, "compose", "nav.newpost", d)
}

// postError turns a refusal into words for the person who posted; "" means it was not a refusal.
func (s *Site) postError(r *http.Request, err error) string {
	tr := s.tr(r)
	switch {
	case errors.Is(err, store.ErrRejected):
		return tr.Str("post.err_rejected")
	case errors.Is(err, store.ErrForbidden):
		return tr.Str("post.err_forbidden")
	case errors.Is(err, store.ErrRateLimit):
		return tr.Str("post.err_slow")
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
	Reported   bool
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
	ro := s.readOnly(r, me)
	d := postData{Post: postViews([]store.Post{p}, me, ro, true, back)[0], Replies: postViews(replies, me, ro, true, back), Newest: newest,
		CanReply: me != nil && !ro && !p.Removed, Error: msg, ReplyDraft: draft, Max: bodyMax, Reported: r.URL.Query().Get("reported") == "1"}
	v := s.base(r, s.tr(r).F("post.title", p.Author.Name), d)
	if p.Body != "" {
		v.Desc = firstRunes(p.Body, 200)
	}
	v.OEmbed = s.cfg.PublicURL + "/posts/" + url.PathEscape(id) + "/oembed.json"
	v.Canonical = s.cfg.PublicURL + "/posts/" + url.PathEscape(id)
	s.renderView(w, v, status, "post")
}

func (s *Site) reply(w http.ResponseWriter, r *http.Request) {
	me, ok := s.canWrite(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, s.tr(r).Str("err.big"), http.StatusRequestEntityTooLarge)
		return
	}
	id := r.PathValue("id")
	n, msg := s.readPost(r)
	if msg == "" {
		p, err := s.store.Reply(r.Context(), me.ID, id, n)
		if err == nil {
			http.Redirect(w, r, "/posts/"+url.PathEscape(id)+"#post-"+url.PathEscape(p.ID), http.StatusSeeOther)
			return
		}
		if msg = s.postError(r, err); msg == "" {
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
	if s.access(r.Context(), me).ReadOnly {
		if wantsJSON {
			http.Error(w, `{"error":"read only"}`, http.StatusForbidden)
			return
		}
		s.fail(w, r, store.ErrForbidden)
		return
	}
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
	me, ok := s.canWrite(w, r)
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
	me, ok := s.canWrite(w, r)
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
	Items        []PostView
	People       []store.Person
	Communities  []store.Community
	ActivityData activityData
	Tab          string
	Pager        Pager
	Signed       bool
}

var profileTabs = map[string]bool{"posts": true, "yeahs": true, "following": true, "followers": true, "friends": true, "activity": true}

func (s *Site) profile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	tab := r.PathValue("tab")
	if tab == "" {
		tab = "posts"
	}
	if !profileTabs[tab] {
		s.fail(w, r, store.ErrNotFound)
		return
	}
	me := s.meView(r)
	viewer := ""
	if me != nil {
		viewer = me.ID
	}
	if id == "me" {
		if me == nil {
			s.need(w, r)
			return
		}
		id = me.ID
		http.Redirect(w, r, "/users/"+url.PathEscape(id)+tabPath(tab), http.StatusSeeOther)
		return
	}
	pr, err := s.store.Profile(r.Context(), viewer, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if pr.Self && me != nil && me.Name != "" {
		pr.Name = me.Name // the console name is cut to ten characters; the account's own name is whole
	}
	page := pageParam(r)
	ro := s.readOnly(r, me)
	base := "/users/" + url.PathEscape(id) + tabPath(tab)
	d := profileData{Profile: pr, Tab: tab, Signed: me != nil}
	var posts []store.Post
	switch tab {
	case "posts":
		posts, err = s.store.UserPosts(r.Context(), viewer, id, pageFor(page))
	case "yeahs":
		posts, err = s.store.Yeahed(r.Context(), viewer, id, pageFor(page))
	case "activity":
		var items []store.ActivityItem
		if items, err = s.store.UserActivity(r.Context(), viewer, id, pageFor(page)); err == nil {
			rows := []ActivityRow{}
			if me != nil {
				rows = s.activityRows(r, *me, items)
			} else {
				for _, a := range items {
					rows = append(rows, ActivityRow{ActivityItem: a})
				}
			}
			d.ActivityData = activityData{Items: rows, Back: base}
			d.Pager = pager(page, len(items) == pageSize, base)
		}
	default:
		d.People, d.Communities, err = s.store.Relations(r.Context(), viewer, id, tab, pageFor(page))
		d.Pager = pager(page, len(d.People) == pageSize, base)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if posts != nil || tab == "posts" || tab == "yeahs" {
		d.Items = postViews(posts, me, ro, false, r.URL.RequestURI())
		d.Pager = pager(page, len(posts) == pageSize, base)
	}
	v := s.base(r, pr.Name, d)
	v.Canonical = s.cfg.PublicURL + "/users/" + url.PathEscape(id)
	s.renderView(w, v, http.StatusOK, "profile")
}

func tabPath(tab string) string {
	if tab == "posts" {
		return ""
	}
	return "/" + tab
}

func (s *Site) followUser(w http.ResponseWriter, r *http.Request) {
	me, ok := s.canWrite(w, r)
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
	s.renderKey(w, r, http.StatusOK, "settings", "menu.settings", settingsData{Settings: st, Saved: r.URL.Query().Get("saved") == "1", Name: me.Name})
}

func (s *Site) saveSettings(w http.ResponseWriter, r *http.Request) {
	me, ok := s.canWrite(w, r)
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
	next.ShowBirthday = r.FormValue("birthday") == "1"
	next.ShowGameSkill = r.FormValue("skill") == "1"
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
	s.renderKey(w, r, http.StatusOK, "welcome", "welcome.title", map[string]string{"Next": next, "Name": me.Name})
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
	s.renderKey(w, r, http.StatusOK, "about", "nav.about", nil)
}

// off explains why OpenVerse is closed to a supervised account whose parent turned it off.
func (s *Site) off(w http.ResponseWriter, r *http.Request) {
	m, ok := s.me(r)
	if !ok {
		http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
		return
	}
	if !s.access(r.Context(), m).Off {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.renderKey(w, r, http.StatusForbidden, "off", "off.title", nil)
}

// ---- topics, related communities ---------------------------------------------------------------

func (s *Site) topic(w http.ResponseWriter, r *http.Request) {
	tag := strings.TrimSpace(r.URL.Query().Get("topic_tag"))
	if tag == "" || utf8.RuneCountInString(tag) > 100 {
		s.fail(w, r, store.ErrNotFound)
		return
	}
	me := s.meView(r)
	viewer := ""
	if me != nil {
		viewer = me.ID
	}
	page := pageParam(r)
	posts, err := s.store.TopicPosts(r.Context(), viewer, tag, pageFor(page))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	d := map[string]any{"Tag": tag, "Items": postViews(posts, me, s.readOnly(r, me), false, r.URL.RequestURI()),
		"Pager": pager(page, len(posts) == pageSize, "/topics?topic_tag="+url.QueryEscape(tag))}
	s.render(w, r, http.StatusOK, "topic", tag, d)
}

func (s *Site) related(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	me := s.meView(r)
	viewer := ""
	if me != nil {
		viewer = me.ID
	}
	parent, err := s.store.Community(r.Context(), viewer, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	subs, err := s.store.SubCommunities(r.Context(), viewer, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "related", s.tr(r).F("related.title", parent.Name), map[string]any{"Parent": parent, "Communities": subs})
}

// ---- data export, spoiler, oEmbed ---------------------------------------------------------------

func (s *Site) export(w http.ResponseWriter, r *http.Request) {
	me, ok := s.need(w, r)
	if !ok {
		return
	}
	b, err := s.store.Export(r.Context(), me.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	noStore(w)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+me.ID+`_user_data.json"`)
	_, _ = w.Write(b)
}

func (s *Site) spoiler(w http.ResponseWriter, r *http.Request) {
	me, ok := s.canWrite(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := s.store.SetSpoiler(r.Context(), me.ID, id, r.FormValue("spoiler") == "1"); err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/posts/"+url.PathEscape(id), http.StatusSeeOther)
}

// oembed answers the link-preview card for a post: who wrote it and where. It carries no media.
func (s *Site) oembed(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := s.store.Post(r.Context(), "", id)
	if err != nil || p.Removed {
		http.NotFound(w, r)
		return
	}
	title := firstRunes(p.Body, 120)
	if title == "" {
		title = s.cat.For(s.lang(r)).F("post.title", p.Author.Name)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_ = json.NewEncoder(w).Encode(map[string]any{"version": "1.0", "type": "link", "title": title, "author_name": p.Author.Name,
		"author_url": s.cfg.PublicURL + "/users/" + url.PathEscape(p.Author.ID), "provider_name": "OpenVerse", "provider_url": s.cfg.PublicURL})
}

func firstRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return string(r)
}

// ---- old addresses ------------------------------------------------------------------------------

// redirects keeps the links of the Juxtaposition web UI working: the same post, community and
// profile ids, under the new paths.
func (s *Site) redirects() {
	to := func(from, dest string) {
		s.mux.HandleFunc("GET "+from, func(w http.ResponseWriter, r *http.Request) {
			target := dest
			for _, k := range []string{"id", "type", "rest"} {
				target = strings.ReplaceAll(target, "{"+k+"}", url.PathEscape(r.PathValue(k)))
			}
			http.Redirect(w, r, target, http.StatusMovedPermanently)
		})
	}
	to("/titles", "/communities")
	to("/titles/all", "/communities")
	to("/titles/{id}", "/communities/{id}")
	to("/titles/{id}/related", "/communities/{id}/related")
	to("/titles/{id}/{type}", "/communities/{id}")
	to("/communities/{id}/{type}", "/communities/{id}")
	to("/users/me", "/me")
	to("/users/me/settings", "/settings")
	to("/activity/settings", "/settings#activity")
	to("/feed/people", "/feed")
	to("/feed/all", "/feed?scope=everyone")
	to("/news", "/notifications")
	to("/news/my_news", "/notifications")
	to("/news/friend_requests", "/notifications")
	to("/logout", "/auth/logout")
	s.mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		if s.legacy != nil && strings.HasPrefix(r.URL.Query().Get("redirect"), "/admin") {
			s.legacy.ServeHTTP(w, r) // moderators signing in to the admin pages
			return
		}
		http.Redirect(w, r, "/auth/login", http.StatusMovedPermanently)
	})
	to("/posts/{id}/report", "/posts/{id}")
	to("/devices/", "/devices")
}
