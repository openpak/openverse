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
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/openpak/openverse/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

const pageSize = 20

// Site is the http.Handler of verse.openpak.org.
type Site struct {
	cfg   Config
	store store.Store
	sso   *http.Client
	cdn   *http.Client
	pages map[string]*template.Template
	mux   *http.ServeMux
	asset string
}

// New builds the site. It does not touch the network.
func New(cfg Config, st store.Store) (*Site, error) {
	s := &Site{cfg: cfg, store: st, sso: &http.Client{Timeout: 10 * time.Second}, cdn: &http.Client{Timeout: 10 * time.Second},
		asset: strconv.FormatInt(time.Now().Unix(), 36)}
	funcs := template.FuncMap{
		"ago":      ago,
		"datetime": func(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") },
		"iso":      func(t time.Time) string { return t.UTC().Format(time.RFC3339) },
		"linkify":  linkify,
		"plural":   plural,
		"initial":  initial,
		"active":   func(path, href string) bool { return path == href || strings.HasPrefix(path, href+"/") },
		"add":      func(a, b int) int { return a + b },
		"sub":      func(a, b int) int { return a - b },
		"count":    count,
		"dict":     dict,
		"catName":  catName,
	}
	layout, err := templateFS.ReadFile("templates/_layout.html")
	if err != nil {
		return nil, err
	}
	parts, err := templateFS.ReadFile("templates/_parts.html")
	if err != nil {
		return nil, err
	}
	s.pages = map[string]*template.Template{}
	entries, _ := fs.ReadDir(templateFS, "templates")
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
		s.pages[strings.TrimSuffix(e.Name(), ".html")] = t
	}

	sub, _ := fs.Sub(staticFS, "static")
	static := http.StripPrefix("/static/", http.FileServerFS(sub))
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("GET /{$}", s.home)
	s.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) { s.fail(w, r, store.ErrNotFound) })
	s.mux.HandleFunc("GET /communities", s.communities)
	s.mux.HandleFunc("GET /communities/{id}", s.community)
	s.mux.HandleFunc("POST /communities/{id}/follow", s.followCommunity)
	s.mux.HandleFunc("GET /feed", s.feed)
	s.mux.HandleFunc("GET /activity", s.activity)
	s.mux.HandleFunc("GET /notifications", s.notifications)
	s.mux.HandleFunc("GET /notifications/count", s.notificationCount)
	s.mux.HandleFunc("GET /new", s.compose)
	s.mux.HandleFunc("POST /new", s.createPost)
	s.mux.HandleFunc("GET /posts/{id}", s.post)
	s.mux.HandleFunc("POST /posts/{id}/reply", s.reply)
	s.mux.HandleFunc("POST /posts/{id}/yeah", s.yeah)
	s.mux.HandleFunc("POST /posts/{id}/delete", s.deletePost)
	s.mux.HandleFunc("POST /posts/{id}/report", s.report)
	s.mux.HandleFunc("GET /me", s.meRedirect)
	s.mux.HandleFunc("GET /users/{id}", s.profile)
	s.mux.HandleFunc("POST /users/{id}/follow", s.followUser)
	s.mux.HandleFunc("GET /settings", s.settings)
	s.mux.HandleFunc("POST /settings", s.saveSettings)
	s.mux.HandleFunc("GET /welcome", s.welcome)
	s.mux.HandleFunc("POST /welcome", s.acceptWelcome)
	s.mux.HandleFunc("GET /about", s.about)
	s.mux.HandleFunc("GET /auth/login", s.authLogin)
	s.mux.HandleFunc("GET /auth/callback", s.authCallback)
	s.mux.HandleFunc("GET /auth/logout", s.authLogout)
	s.mux.HandleFunc("GET /cdn/{path...}", s.cdnProxy)
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintln(w, "ok openverse") })
	s.mux.HandleFunc("GET /static/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		static.ServeHTTP(w, r)
	})
	return s, nil
}

// ServeHTTP adds the security headers and refuses cross-site writes.
func (s *Site) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
	if r.Method == http.MethodPost && !sameSite(r) {
		http.Error(w, "cross-site request refused", http.StatusForbidden)
		return
	}
	s.mux.ServeHTTP(w, r)
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
	Title    string
	Desc     string
	Path     string
	Asset    string
	OpenPak  string
	Contact  string
	SignIn   bool
	Me       *Me
	Unread   int
	Next     string // where Sign in returns to
	Data     any
	Flash    string
	NotFound bool
}

func (s *Site) base(r *http.Request, title string, data any) view {
	v := view{Title: title, Path: r.URL.Path, Asset: s.asset, OpenPak: s.cfg.OpenPakURL, Contact: s.cfg.ContactEmail,
		SignIn: s.signInOn(), Data: data, Desc: "OpenVerse is OpenPak's community space: posts, drawings and conversations about the games you play.",
		Next: r.URL.RequestURI()}
	if m, ok := s.me(r); ok {
		v.Me = &m
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

func (s *Site) render(w http.ResponseWriter, r *http.Request, status int, name, title string, data any) {
	s.renderView(w, s.base(r, title, data), status, name)
}

func (s *Site) renderView(w http.ResponseWriter, v view, status int, name string) {
	var buf bytes.Buffer
	if err := s.pages[name].ExecuteTemplate(&buf, "page", v); err != nil {
		log.Printf("openverse: render %s: %v", name, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// fail answers an error: a friendly page, nothing of the error itself reaches the visitor.
func (s *Site) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.render(w, r, http.StatusNotFound, "error", "Not found", "That page does not exist, or the thing it showed is gone.")
	case errors.Is(err, store.ErrForbidden):
		s.render(w, r, http.StatusForbidden, "error", "Not allowed", "This account cannot do that here. If it is supervised, a parent or guardian controls what it may post.")
	case errors.Is(err, store.ErrRateLimit):
		s.render(w, r, http.StatusTooManyRequests, "error", "Slow down", "You are doing that a little fast. Wait a moment and try again.")
	default:
		log.Printf("openverse: %s %s: %v", r.Method, r.URL.Path, err)
		s.render(w, r, http.StatusServiceUnavailable, "error", "Temporarily unavailable", "OpenVerse is not answering right now. Try again in a minute.")
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

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

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

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
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
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(body)
}

// catName is the words for an activity category id.
func catName(id string) string {
	switch id {
	case "badge":
		return "Badges earned"
	case "splatfest":
		return "Splatfest results"
	case "kart":
		return "Mario Kart results"
	case "maker":
		return "Courses uploaded"
	case "trophy":
		return "Trophies"
	case "trade":
		return "Trades"
	}
	return id
}
