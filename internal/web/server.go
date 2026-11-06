// Package web is OpenVerse's website: OpenPak's own community space, over a store.Store, with
// sign-in through openpak.org single sign-on. Pages are html/template in the shared Open-site shell.
package web

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/openpak/openverse/internal/i18n"
	"github.com/openpak/openverse/internal/linkcodes"
	"github.com/openpak/openverse/internal/pgkv"
	"github.com/openpak/openverse/internal/names"
	"github.com/openpak/openverse/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

const pageSize = 20

// Site is the http.Handler of verse.openpak.org.
type Site struct {
	cfg    Config
	store  store.Store
	sso    *http.Client
	cdn    *http.Client
	cat    *i18n.Catalog
	pages  map[string]map[string]*template.Template // language -> page
	mux    *http.ServeMux
	asset  string
	codes  linkcodes.KV
	legacy http.Handler // the Juxtaposition UI, for /admin; nil when not configured
	tries  tryLimiter
	names  *names.Book

	accMu sync.Mutex
	acc   map[string]accessEntry
}

type accessEntry struct {
	v  store.Viewer
	at time.Time
}

// SetCodes sets where device link codes live (Postgres in production, memory in tests and the demo).
func (s *Site) SetCodes(kv linkcodes.KV) { s.codes = kv }

// New builds the site. It does not touch the network.
func New(cfg Config, st store.Store) (*Site, error) {
	s := &Site{cfg: cfg, store: st, sso: &http.Client{Timeout: 10 * time.Second}, cdn: &http.Client{Timeout: 10 * time.Second},
		cat: i18n.Load(), asset: strconv.FormatInt(time.Now().Unix(), 36), acc: map[string]accessEntry{}}
	if cfg.DatabaseURL != "" {
		kv, err := pgkv.Open(context.Background(), cfg.DatabaseURL)
		if err != nil {
			return nil, err
		}
		s.codes = kv
		s.names = names.New(kv)
	} else if cfg.Demo {
		s.names = names.New(names.NewMemory())
		mem := linkcodes.NewMemory()
		_ = mem.Set(context.Background(), "verse-lite:code:ABC234", `{"deviceClass":"wii","secretHash":"x","ip":"127.0.0.1","createdAt":`+strconv.FormatInt(time.Now().UnixMilli(), 10)+`,"accountId":null}`, 600)
		s.codes = mem
	}
	if cfg.LegacyURL != "" {
		u, err := url.Parse(cfg.LegacyURL)
		if err != nil {
			return nil, err
		}
		s.legacy = &httputil.ReverseProxy{
			Rewrite: func(r *httputil.ProxyRequest) {
				r.SetURL(u)
				r.Out.Host = r.In.Host // the UI builds its redirects from the public host
				r.SetXForwarded()
			},
		}
	}
	s.store = &namedStore{Store: st, book: s.names}
	layout, err := templateFS.ReadFile("templates/_layout.html")
	if err != nil {
		return nil, err
	}
	parts, err := templateFS.ReadFile("templates/_parts.html")
	if err != nil {
		return nil, err
	}
	s.pages = map[string]map[string]*template.Template{}
	entries, _ := fs.ReadDir(templateFS, "templates")
	for _, lang := range s.cat.Languages() {
		tr := s.cat.For(lang)
		funcs := template.FuncMap{
			"t":            tr.Str,
			"tf":           tr.F,
			"tn":           tr.N,
			"tn1":          tr.W,
			"th":           tr.H,
			"thn":          tr.HN,
			"ago":          func(t time.Time) string { return agoIn(tr, t) },
			"datetime":     func(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") },
			"iso":          func(t time.Time) string { return t.UTC().Format(time.RFC3339) },
			"linkify":      linkify,
			"initial":      initial,
			"active":       func(path, href string) bool { return path == href || strings.HasPrefix(path, href+"/") },
			"add":          func(a, b int) int { return a + b },
			"sub":          func(a, b int) int { return a - b },
			"count":        count,
			"urlq":         url.QueryEscape,
			"dict":         dict,
			"catName":      func(id string) string { return catName(tr, id) },
			"commSub":      func(c store.Community) string { return commSub(tr, c) },
			"chipLabel":    chipLabel,
			"tileHref":     tileHref,
			"actors":       func(n store.Notification) template.HTML { return actorsHTML(tr, n) },
			"activityText": func(a ActivityRow) string { return activityText(tr, a.ActivityItem) },
		}
		s.pages[lang] = map[string]*template.Template{}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "_") {
				continue
			}
			body, err := templateFS.ReadFile("templates/" + e.Name())
			if err != nil {
				return nil, err
			}
			t, err := template.New(e.Name()).Funcs(funcs).Parse(string(layout) + string(parts) + string(body))
			if err != nil {
				return nil, fmt.Errorf("%s: %w", e.Name(), err)
			}
			s.pages[lang][strings.TrimSuffix(e.Name(), ".html")] = t
		}
	}

	sub, _ := fs.Sub(staticFS, "static")
	static := http.StripPrefix("/static/", http.FileServerFS(sub))
	s.mux = http.NewServeMux()
	h := s.mux.HandleFunc
	h("GET /{$}", s.home)
	h("GET /", func(w http.ResponseWriter, r *http.Request) { s.fail(w, r, store.ErrNotFound) })
	h("GET /communities", s.communities)
	h("GET /communities/{id}", s.community)
	h("GET /communities/{id}/related", s.related)
	h("POST /communities/{id}/follow", s.followCommunity)
	h("GET /topics", s.topic)
	h("GET /feed", s.feed)
	h("GET /activity", s.activity)
	h("POST /activity/{id}/delete", s.deleteActivity)
	h("GET /notifications", s.notifications)
	h("GET /notifications/count", s.notificationCount)
	h("GET /new", s.compose)
	h("POST /new", s.createPost)
	h("GET /posts/{id}", s.post)
	h("GET /posts/{id}/oembed.json", s.oembed)
	h("POST /posts/{id}/reply", s.reply)
	h("POST /posts/{id}/yeah", s.yeah)
	h("POST /posts/{id}/delete", s.deletePost)
	h("POST /posts/{id}/spoiler", s.spoiler)
	h("POST /posts/{id}/report", s.report)
	h("GET /me", s.meRedirect)
	h("GET /users/downloadUserData.json", s.export)
	h("GET /users/{id}", s.profile)
	h("GET /users/{id}/{tab}", s.profile)
	h("POST /users/{id}/follow", s.followUser)
	h("GET /settings", s.settings)
	h("POST /settings", s.saveSettings)
	h("GET /devices", s.devices)
	h("POST /devices/check", s.devicesCheck)
	h("POST /devices/approve", s.devicesApprove)
	h("POST /devices/revoke", s.devicesRevoke)
	h("GET /welcome", s.welcome)
	h("POST /welcome", s.acceptWelcome)
	h("GET /welcome/off", s.off)
	h("GET /about", s.about)
	h("GET /auth/login", s.authLogin)
	h("GET /auth/callback", s.authCallback)
	h("GET /auth/logout", s.authLogout)
	h("GET /cdn/{path...}", s.cdnProxy)
	h("GET /robots.txt", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, "User-agent: *\nAllow: /\nDisallow: /admin\nDisallow: /auth/\nDisallow: /new\nDisallow: /settings\nDisallow: /devices\n")
	})
	h("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/static/favicon-32.png", http.StatusMovedPermanently)
	})
	h("GET /manifest.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		fmt.Fprint(w, `{"name":"OpenVerse","short_name":"OpenVerse","start_url":"/feed","scope":"/","display":"standalone","background_color":"#24272d","theme_color":"#24272d",`+
			`"icons":[{"src":"/static/openverse.svg","sizes":"any","type":"image/svg+xml"},{"src":"/static/apple-touch-icon.png","sizes":"180x180","type":"image/png"}]}`)
	})
	h("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintln(w, "ok openverse") })
	h("GET /static/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		static.ServeHTTP(w, r)
	})
	s.redirects()
	if s.legacy != nil {
		// Moderation is still the Juxtaposition UI's: /admin and the sign-in it uses go to it unchanged.
		for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
			s.mux.Handle(m+" /admin", s.legacy)
			s.mux.Handle(m+" /admin/{rest...}", s.legacy)
		}
		s.mux.Handle("GET /login/callback", s.legacy)
	}
	return s, nil
}

// ServeHTTP adds the security headers and refuses cross-site writes.
func (s *Site) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	if !s.legacyPath(r) { // the admin pages bring their own policy
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	}
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
	if l := r.URL.Query().Get("lang"); l != "" {
		if got := s.cat.Pick(l); got != "" {
			http.SetCookie(w, &http.Cookie{Name: langCookie, Value: got, Path: "/", MaxAge: 365 * 24 * 3600, SameSite: http.SameSiteLaxMode, Secure: s.secure()})
			r.AddCookie(&http.Cookie{Name: langCookie, Value: got})
		}
	}
	if r.Method == http.MethodPost && !sameSite(r) {
		http.Error(w, "cross-site request refused", http.StatusForbidden)
		return
	}
	if !s.legacyPath(r) && s.parentOff(w, r) {
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Site) legacyPath(r *http.Request) bool {
	if s.legacy == nil {
		return false
	}
	p := r.URL.Path
	return p == "/admin" || strings.HasPrefix(p, "/admin/") || p == "/login" || strings.HasPrefix(p, "/login/")
}

// parentOff stops a signed-in account whose parent turned OpenVerse off: it may see the explanation
// and sign out, nothing else. The decision is the miiverse-api's (the same parental lookup it makes
// for every request); a failed lookup there already means off.
func (s *Site) parentOff(w http.ResponseWriter, r *http.Request) bool {
	switch p := r.URL.Path; {
	case p == "/welcome/off", strings.HasPrefix(p, "/auth/"), strings.HasPrefix(p, "/static/"), strings.HasPrefix(p, "/cdn/"), p == "/healthz", p == "/robots.txt":
		return false
	}
	m, ok := s.me(r)
	if !ok {
		return false
	}
	if v := s.access(r.Context(), m); v.Off {
		http.Redirect(w, r, "/welcome/off", http.StatusSeeOther)
		return true
	}
	return false
}

const accessTTL = 20 * time.Second

// access is what the person may do, cached for a few seconds so a page does not cost a parental
// lookup per request. If the lookup fails the account is treated as read-only (never as full).
func (s *Site) access(ctx context.Context, m Me) store.Viewer {
	s.accMu.Lock()
	e, hit := s.acc[m.ID]
	s.accMu.Unlock()
	if hit && time.Since(e.at) < accessTTL {
		return e.v
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	v, err := s.store.Viewer(cctx, m.ID)
	if err != nil {
		if hit {
			return e.v
		}
		return store.Viewer{ID: m.ID, ReadOnly: true}
	}
	s.accMu.Lock()
	if len(s.acc) > 5000 {
		s.acc = map[string]accessEntry{}
	}
	s.acc[m.ID] = accessEntry{v: v, at: time.Now()}
	s.accMu.Unlock()
	return v
}

// sameSite is the CSRF guard: a write must come from a page of this site. Browsers send
// Sec-Fetch-Site; older ones send Origin. A request with neither is a script, not a browser form.
func sameSite(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "":
	default:
		return false
	}
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		return err == nil && u.Host == r.Host
	}
	return true
}

// view is what every template receives.
type view struct {
	Title     string
	Desc      string
	Path      string
	Asset     string
	OpenPak   string
	Contact   string
	Lang      string
	Canonical string
	Site      string
	OEmbed    string
	SignIn    bool
	Me        *Me
	ReadOnly  bool // a supervised account that may read but not post
	Unread    int
	Onboard   bool   // signed in but the rules are not accepted yet: the shell shows no navigation
	Next      string // where Sign in returns to
	Data      any
	Flash     string
	NotFound  bool
}

const langCookie = "openverse_lang"

// lang is the visitor's language: the ?lang= choice remembered in a cookie, else Accept-Language.
func (s *Site) lang(r *http.Request) string {
	choice := ""
	if c, err := r.Cookie(langCookie); err == nil {
		choice = c.Value
	}
	return s.cat.Match(choice, r.Header.Get("Accept-Language"))
}

func (s *Site) tr(r *http.Request) i18n.T { return s.cat.For(s.lang(r)) }

func (s *Site) base(r *http.Request, title string, data any) view {
	tr := s.tr(r)
	v := view{Title: title, Path: r.URL.Path, Asset: s.asset, OpenPak: s.cfg.OpenPakURL, Contact: s.cfg.ContactEmail, Lang: tr.Lang,
		SignIn: s.signInOn(), Data: data, Desc: tr.Str("site.desc"), Next: r.URL.RequestURI(), Canonical: s.cfg.PublicURL + r.URL.Path, Site: s.cfg.PublicURL}
	if m, ok := s.me(r); ok {
		v.Me = &m
		v.Onboard = !m.Rules
		v.ReadOnly = s.access(r.Context(), m).ReadOnly
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if n, err := s.store.UnreadNotifications(ctx, m.ID); err == nil {
			v.Unread = n
		}
	}
	if !strings.HasPrefix(v.Next, "/") {
		v.Next = "/"
	}
	return v
}

// render shows a page. title is shown as given; use s.titleKey for a catalogue key.
func (s *Site) render(w http.ResponseWriter, r *http.Request, status int, name, title string, data any) {
	s.renderView(w, s.base(r, title, data), status, name)
}

// renderKey is render with the title (and, for the error page, nothing else) taken from a key.
func (s *Site) renderKey(w http.ResponseWriter, r *http.Request, status int, name, titleKey string, data any) {
	s.render(w, r, status, name, s.tr(r).Str(titleKey), data)
}

func (s *Site) renderView(w http.ResponseWriter, v view, status int, name string) {
	t := s.pages[v.Lang][name]
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "page", v); err != nil {
		log.Printf("openverse: render %s: %v", name, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Language", v.Lang)
	w.Header().Set("Vary", "Accept-Language, Cookie")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// errorPage answers with the friendly page for a catalogue entry err.<what>.{h,p}.
func (s *Site) errorPage(w http.ResponseWriter, r *http.Request, status int, what string) {
	tr := s.tr(r)
	s.render(w, r, status, "error", tr.Str("err."+what+".h"), tr.Str("err."+what+".p"))
}

// fail answers an error: a friendly page, nothing of the error itself reaches the visitor.
func (s *Site) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.errorPage(w, r, http.StatusNotFound, "notfound")
	case errors.Is(err, store.ErrForbidden):
		s.errorPage(w, r, http.StatusForbidden, "forbidden")
	case errors.Is(err, store.ErrRateLimit):
		s.errorPage(w, r, http.StatusTooManyRequests, "slow")
	default:
		log.Printf("openverse: %s %s: %v", r.Method, r.URL.Path, err)
		s.errorPage(w, r, http.StatusServiceUnavailable, "down")
	}
}

// need returns the signed-in visitor, or sends them to sign in (and answers false).
func (s *Site) need(w http.ResponseWriter, r *http.Request) (Me, bool) {
	m, ok := s.me(r)
	if !ok {
		http.Redirect(w, r, "/auth/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return Me{}, false
	}
	if !m.Rules && r.URL.Path != "/welcome" {
		http.Redirect(w, r, "/welcome?next="+url.QueryEscape(localPath(r.URL.RequestURI())), http.StatusSeeOther)
		return Me{}, false
	}
	return m, true
}

// canWrite is need plus: the account is not supervised read-only. A refused write is a normal page.
func (s *Site) canWrite(w http.ResponseWriter, r *http.Request) (Me, bool) {
	m, ok := s.need(w, r)
	if !ok {
		return m, false
	}
	if s.access(r.Context(), m).ReadOnly {
		s.fail(w, r, store.ErrForbidden)
		return m, false
	}
	return m, true
}

func pageParam(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || n < 1 || n > 10000 {
		return 1
	}
	return n
}

// Pager is a newer/older pair for a list.
type Pager struct {
	Page       int
	Prev, Next string
}

func pager(page int, more bool, base string) Pager {
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	p := Pager{Page: page}
	if page > 1 {
		p.Prev = fmt.Sprintf("%s%spage=%d", base, sep, page-1)
	}
	if more {
		p.Next = fmt.Sprintf("%s%spage=%d", base, sep, page+1)
	}
	return p
}

func pageFor(page int) store.Page { return store.Page{Limit: pageSize, Offset: (page - 1) * pageSize} }

func count(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 10_000:
		return fmt.Sprintf("%dk", n/1000)
	}
	return strconv.Itoa(n)
}

func initial(name string) string {
	for _, r := range name {
		return strings.ToUpper(string(r))
	}
	return "?"
}

func dict(kv ...any) map[string]any {
	m := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[fmt.Sprint(kv[i])] = kv[i+1]
	}
	return m
}

func agoIn(tr i18n.T, t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return tr.Str("time.now")
	case d < time.Hour:
		return tr.F("time.min", int(d.Minutes()))
	case d < 48*time.Hour:
		return tr.F("time.hour", int(d.Hours()))
	case d < 60*24*time.Hour:
		return tr.F("time.days", int(d.Hours()/24))
	}
	return t.UTC().Format("2006-01-02")
}

var urlRe = regexp.MustCompile(`https?://[^\s<>"']+`)

// linkify escapes a post body and turns http(s) addresses into links. Nothing else is interpreted:
// a post is plain text.
func linkify(body string) template.HTML {
	var b strings.Builder
	last := 0
	for _, loc := range urlRe.FindAllStringIndex(body, -1) {
		b.WriteString(template.HTMLEscapeString(body[last:loc[0]]))
		raw := strings.TrimRight(body[loc[0]:loc[1]], ".,;:!?)")
		end := loc[0] + len(raw)
		esc := template.HTMLEscapeString(raw)
		b.WriteString(`<a href="` + esc + `" rel="noopener nofollow ugc" target="_blank">` + esc + `</a>`)
		last = end
	}
	b.WriteString(template.HTMLEscapeString(body[last:]))
	return template.HTML(b.String())
}

// cdnProxy serves pictures (drawings, icons, avatars) from the API's media store. Browsers cannot
// reach the API's own host, and the API only answers for these prefixes.
func (s *Site) cdnProxy(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("path")
	ok := false
	for _, p := range []string{"paintings/", "screenshots/", "icons/", "headers/", "mii/"} {
		if strings.HasPrefix(key, p) {
			ok = true
		}
	}
	if !ok || strings.Contains(key, "..") || strings.Contains(key, "\\") {
		http.NotFound(w, r)
		return
	}
	media, hit := s.store.(interface {
		Media(ctx context.Context, key string) (contentType string, body []byte, err error)
	})
	if !hit {
		http.NotFound(w, r)
		return
	}
	ct, body, err := media.Media(r.Context(), key)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
		} else {
			http.Error(w, "bad gateway", http.StatusBadGateway)
		}
		return
	}
	w.Header().Set("Content-Type", ct)
	// Post media can be deleted, so it is revalidated; icons and faces change rarely.
	if strings.HasPrefix(key, "paintings/") || strings.HasPrefix(key, "screenshots/") {
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=86400")
	}
	_, _ = w.Write(body)
}

// catName is the words for an activity category id.
func catName(tr i18n.T, id string) string {
	if s := tr.Str("cat." + id); s != "cat."+id {
		return s
	}
	return id
}

// commSub is the line under a community tile: the consoles of a game, then its posts once it has
// some; a room says how many followers it has. A quiet game does not announce that it is quiet.
func commSub(tr i18n.T, c store.Community) string {
	var parts []string
	if c.Family {
		parts = append(parts, tr.N("n.games", len(c.Members)))
	} else if c.ParentName != "" && c.ParentName != c.Name {
		parts = append(parts, c.ParentName) // a game found by name or followed on its own says which family it is in
	}
	if len(c.Consoles) > 0 && !c.Family {
		parts = append(parts, strings.Join(c.Consoles, " · "))
	}
	switch {
	case c.Posts > 0:
		parts = append(parts, tr.N("n.posts", c.Posts))
	case len(parts) == 0 && c.Followers > 0:
		parts = append(parts, tr.N("n.followers", c.Followers))
	}
	return strings.Join(parts, " · ")
}

// chipLabel is a game's chip on its family page: the game and where it is played.
func chipLabel(c store.Community) string {
	if len(c.Consoles) == 0 {
		return c.Name
	}
	return c.Name + " \u00b7 " + strings.Join(c.Consoles, ", ")
}

// tileHref is where a community tile leads. A game under a family opens the family page with its chip
// chosen when the tile is a link to a community page; the composer takes the game's own id.
func tileHref(link string, c store.Community) string {
	if c.Parent != "" && strings.HasPrefix(link, "/communities/") {
		return "/communities/" + url.PathEscape(c.Parent) + "?game=" + url.QueryEscape(c.ID)
	}
	return fmt.Sprintf(link, c.ID)
}

// activityText is the sentence after the person's name for an activity post. A category this build
// has no sentence for is shown as its subject, never as a raw key.
func activityText(tr i18n.T, a store.ActivityItem) string {
	if a.Digest {
		return tr.F("activity.text.digest", a.Count)
	}
	if s := tr.Str("activity.text." + a.Category); s != "activity.text."+a.Category {
		return fmt.Sprintf(s, a.Subject)
	}
	return a.Subject
}

// actorsHTML is "<b>A</b>, <b>B</b> and 3 others" for a notification, names escaped.
func actorsHTML(tr i18n.T, n store.Notification) template.HTML {
	var b strings.Builder
	for i, a := range n.Actors {
		switch {
		case i == 0:
		case i == len(n.Actors)-1 && n.Others == 0:
			b.WriteString(" " + template.HTMLEscapeString(tr.Str("notes.and")) + " ")
		default:
			b.WriteString(", ")
		}
		b.WriteString("<b>" + template.HTMLEscapeString(a.Name) + "</b>")
	}
	if n.Others > 0 {
		b.WriteString(" " + template.HTMLEscapeString(tr.Str("notes.and")) + " " + template.HTMLEscapeString(tr.N("notes.others", n.Others)))
	}
	return template.HTML(b.String())
}
