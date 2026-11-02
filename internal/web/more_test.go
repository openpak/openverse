package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/openpak/openverse/internal/demo"
)

func doLang(t *testing.T, s *Site, lang, target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", target, nil)
	r.Header.Set("Accept-Language", lang)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

var pagesToCheck = []string{"/communities", "/communities/101", "/communities/201", "/communities/201?game=111", "/communities/203?game=130&sort=popular", "/communities/102/related", "/new?community=201", "/feed", "/feed?scope=everyone", "/activity", "/activity?scope=following",
	"/notifications", "/new", "/new?community=101", "/posts/p101", "/users/1000000005", "/users/1000000005/yeahs", "/users/1000000005/following",
	"/users/1000000005/followers", "/users/1000000005/friends", "/users/1000000005/activity", "/settings", "/devices", "/about", "/topics?topic_tag=x"}

// No raw catalogue key, no leftover template syntax and no untranslated English UI text may reach
// a page in a translated language.
func TestTranslatedPagesHaveNoRawKeys(t *testing.T) {
	s := newSite(t)
	c := signIn(t, s, "")
	rawKey := regexp.MustCompile(`>\s*(?:nav|menu|foot|post|report|about|err|communities|community|feed|activity|compose|notes|profile|welcome|settings|devices|dev|invite|btn|pager|time|topic|related|off|cat|n)\.[a-z0-9_.]+\s*<|\{\{|%!|%[sd]`)
	for _, lang := range []string{"en", "es", "fr", "de", "ja"} {
		for _, p := range pagesToCheck {
			w := doLang(t, s, lang, p, c)
			if w.Code != 200 {
				t.Errorf("%s %s: status %d", lang, p, w.Code)
				continue
			}
			body := w.Body.String()
			if m := rawKey.FindString(body); m != "" {
				t.Errorf("%s %s: raw key or format left in the page: %q", lang, p, m)
			}
			want := lang
			if lang == "ja" {
				want = "en"
			}
			if !strings.Contains(body, `<html lang="`+want+`">`) {
				t.Errorf("%s %s: wrong html lang", lang, p)
			}
		}
	}
}

func TestLanguagesActuallyDiffer(t *testing.T) {
	s := newSite(t)
	for lang, want := range map[string]string{"es": "Comunidades", "fr": "Communautés", "de": "Communitys", "en": "Communities", "ja": "Communities"} {
		if body := doLang(t, s, lang, "/communities").Body.String(); !strings.Contains(body, ">"+want+"<") {
			t.Errorf("%s: /communities does not say %q", lang, want)
		}
	}
	if body := doLang(t, s, "es", "/about").Body.String(); strings.Contains(body, "One account") || !strings.Contains(body, "Una sola cuenta") {
		t.Error("about is not translated to es")
	}
}

func TestLangQueryIsRemembered(t *testing.T) {
	s := newSite(t)
	w := doLang(t, s, "en", "/about?lang=fr")
	if !strings.Contains(w.Body.String(), "À propos") {
		t.Fatal("?lang=fr did not switch")
	}
	var ck *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == langCookie {
			ck = c
		}
	}
	if ck == nil || ck.Value != "fr" {
		t.Fatal("language not remembered")
	}
	if !strings.Contains(doLang(t, s, "en", "/about", ck).Body.String(), "À propos") {
		t.Error("cookie not honoured")
	}
	if w := doLang(t, s, "en", "/about?lang=xx"); strings.Contains(w.Header().Get("Set-Cookie"), langCookie) {
		t.Error("an unknown language must not be stored")
	}
}

func TestLinkedDevicesFlow(t *testing.T) {
	s := newSite(t)
	c := signIn(t, s, "")
	page := do(t, s, "GET", "/devices", nil, c)
	body := page.Body.String()
	for _, want := range []string{"Linked devices", `name="code"`, "Where do I find the code?", "Your linked devices", "PlayStation 3", "Xbox 360", ">Remove<", "lite.verse.openpak.org"} {
		if !strings.Contains(body, want) {
			t.Errorf("devices page lacks %q", want)
		}
	}
	// It lives in the account menu, never the top navigation.
	nav := do(t, s, "GET", "/feed", nil, c).Body.String()
	top := nav[strings.Index(nav, `class="navlinks"`):strings.Index(nav, `</nav>`)]
	if strings.Contains(top, "/devices") {
		t.Error("Linked devices must not be in the top bar")
	}
	if !strings.Contains(nav[strings.Index(nav, `class="acct-menu"`):], `href="/devices"`) {
		t.Error("Linked devices belongs in the account menu")
	}
	k := regexp.MustCompile(`name="k" value="([^"]+)"`).FindStringSubmatch(body)[1]
	// A bad token is refused.
	if w := do(t, s, "POST", "/devices/check", url.Values{"code": {"ABC234"}, "k": {"nope"}}, c); w.Header().Get("Location") != "/devices?n=bad" {
		t.Errorf("bad token: %d %q", w.Code, w.Header().Get("Location"))
	}
	// An unknown code.
	if w := do(t, s, "POST", "/devices/check", url.Values{"code": {"ZZZZZZ"}, "k": {k}}, c); w.Header().Get("Location") != "/devices?n=bad" {
		t.Errorf("unknown code: %q", w.Header().Get("Location"))
	}
	if !strings.Contains(do(t, s, "GET", "/devices?n=bad", nil, c).Body.String(), "We could not find that code") {
		t.Error("the bad-code note is missing")
	}
	// The demo code: check shows what is asking, approve links it.
	w := do(t, s, "POST", "/devices/check", url.Values{"code": {"abc-234"}, "k": {k}}, c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "A Wii asked to be linked") {
		t.Fatalf("confirm step: %d", w.Code)
	}
	if w := do(t, s, "POST", "/devices/approve", url.Values{"code": {"ABC234"}, "k": {k}}, c); w.Header().Get("Location") != "/devices?n=approved" {
		t.Errorf("approve: %q", w.Header().Get("Location"))
	}
	if w := do(t, s, "POST", "/devices/approve", url.Values{"code": {"ABC234"}, "k": {k}}, c); w.Header().Get("Location") != "/devices?n=bad" {
		t.Errorf("a code approves once: %q", w.Header().Get("Location"))
	}
	// Remove.
	if w := do(t, s, "POST", "/devices/revoke", url.Values{"id": {"d1"}, "k": {k}}, c); w.Header().Get("Location") != "/devices?n=removed" {
		t.Errorf("revoke: %q", w.Header().Get("Location"))
	}
	if strings.Contains(do(t, s, "GET", "/devices", nil, c).Body.String(), "<strong>PlayStation 3</strong>") {
		t.Error("the removed device is still listed")
	}
	// Signed out goes to sign-in.
	if w := do(t, s, "GET", "/devices", nil); w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/auth/login") {
		t.Errorf("signed out: %d", w.Code)
	}
}

func TestDeviceCodesAreRateLimited(t *testing.T) {
	s := newSite(t)
	c := signIn(t, s, "")
	body := do(t, s, "GET", "/devices", nil, c).Body.String()
	k := regexp.MustCompile(`name="k" value="([^"]+)"`).FindStringSubmatch(body)[1]
	slow := false
	for i := 0; i < 14; i++ {
		if do(t, s, "POST", "/devices/check", url.Values{"code": {"ZZZZZZ"}, "k": {k}}, c).Header().Get("Location") == "/devices?n=slow" {
			slow = true
		}
	}
	if !slow {
		t.Error("guessing codes must be rate limited")
	}
}

func siteWithMode(t *testing.T, mode string) (*Site, *http.Cookie) {
	t.Helper()
	d := demo.New()
	d.Mode = mode
	cfg := Config{Demo: true, SessionKey: strings.Repeat("k", 32), PublicURL: "http://example.test", OpenPakURL: "https://openpak.org", ContactEmail: "c@example.test"}
	s, err := New(cfg, d)
	if err != nil {
		t.Fatal(err)
	}
	return s, signIn(t, s, "")
}

// Parental levels: none (off), read (read only) and read+post (full). Reads of an unsupervised or
// read+post account are untouched; "read" hides every way to write and refuses a forced request.
func TestParentalLevels(t *testing.T) {
	// off: everything but the explanation and sign-out goes to /welcome/off.
	s, c := siteWithMode(t, "off")
	for _, p := range []string{"/feed", "/communities", "/posts/p101", "/settings", "/devices", "/users/1000000005"} {
		if w := do(t, s, "GET", p, nil, c); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/welcome/off" {
			t.Errorf("off: %s -> %d %q", p, w.Code, w.Header().Get("Location"))
		}
	}
	if w := do(t, s, "POST", "/new", url.Values{"community": {"101"}, "body": {"hi"}}, c); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/welcome/off" {
		t.Errorf("off: a post must not go through: %d %q", w.Code, w.Header().Get("Location"))
	}
	off := do(t, s, "GET", "/welcome/off", nil, c)
	if off.Code != http.StatusForbidden || !strings.Contains(off.Body.String(), "turned off for this account") || !strings.Contains(off.Body.String(), "/auth/logout") {
		t.Errorf("off page: %d", off.Code)
	}
	if w := do(t, s, "GET", "/auth/logout", nil, c); w.Code != http.StatusSeeOther {
		t.Errorf("sign-out must still work: %d", w.Code)
	}

	// read: pages render, nothing offers a write, forced writes are refused.
	s, c = siteWithMode(t, "read")
	for _, p := range []string{"/feed", "/communities/101", "/posts/p101", "/users/1000000005"} {
		body := do(t, s, "GET", p, nil, c).Body.String()
		for _, no := range []string{`action="/posts/p101/yeah"`, `class="btn primary" href="/new"`, `/follow"`, `class="card composer reply-box"`, `Post here`} {
			if strings.Contains(body, no) {
				t.Errorf("read: %s offers a write (%q)", p, no)
			}
		}
	}
	if !strings.Contains(do(t, s, "GET", "/new?community=101", nil, c).Body.String(), "Posting is turned off for this account") {
		t.Error("read: the composer must say posting is off")
	}
	for _, f := range []struct {
		path string
		form url.Values
	}{
		{"/new", url.Values{"community": {"101"}, "body": {"hi"}}},
		{"/posts/p101/reply", url.Values{"body": {"hi"}}},
		{"/posts/p101/yeah", url.Values{"add": {"1"}}},
		{"/communities/101/follow", url.Values{"follow": {"1"}}},
		{"/users/1000000005/follow", url.Values{"follow": {"1"}}},
		{"/settings", url.Values{"activity": {"1"}}},
		{"/posts/p101/report", url.Values{"reason": {"9"}}},
	} {
		if w := do(t, s, "POST", f.path, f.form, c); w.Code != http.StatusForbidden {
			t.Errorf("read: forced POST %s answered %d, want 403", f.path, w.Code)
		}
	}
	if !strings.Contains(do(t, s, "GET", "/settings", nil, c).Body.String(), "supervised") {
		t.Error("read: settings must say the account is supervised")
	}

	// full: everything is offered.
	s, c = siteWithMode(t, "")
	body := do(t, s, "GET", "/communities/101", nil, c).Body.String()
	if !strings.Contains(body, "Post here") || !strings.Contains(body, `/follow"`) {
		t.Error("full access must offer posting and following")
	}
}

// The addresses of the Juxtaposition web UI keep working.
func TestOldAddressesRedirect(t *testing.T) {
	s := newSite(t)
	for from, to := range map[string]string{
		"/titles":             "/communities",
		"/titles/101":         "/communities/101",
		"/titles/101/hot":     "/communities/101",
		"/titles/101/related": "/communities/101/related",
		"/users/me":           "/me",
		"/users/me/settings":  "/settings",
		"/activity/settings":  "/settings#activity",
		"/feed/people":        "/feed",
		"/feed/all":           "/feed?scope=everyone",
		"/news/my_news":       "/notifications",
		"/login":              "/auth/login",
		"/logout":             "/auth/logout",
	} {
		w := do(t, s, "GET", from, nil)
		if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != to {
			t.Errorf("%s -> %d %q, want %s", from, w.Code, w.Header().Get("Location"), to)
		}
	}
	c := signIn(t, s, "")
	if w := do(t, s, "GET", "/users/me/yeahs", nil, c); w.Header().Get("Location") != "/users/1800000001/yeahs" {
		t.Errorf("/users/me/yeahs -> %q", w.Header().Get("Location"))
	}
}

func TestActivitySettingsAreInSettings(t *testing.T) {
	s := newSite(t)
	c := signIn(t, s, "")
	body := do(t, s, "GET", "/settings", nil, c).Body.String()
	for _, want := range []string{`id="activity"`, `name="activity"`, `name="category"`, "Share my game activity", "Download my data", "Show my birthday"} {
		if !strings.Contains(body, want) {
			t.Errorf("settings lack %q", want)
		}
	}
}

func TestOEmbedAndTopic(t *testing.T) {
	s := newSite(t)
	w := do(t, s, "GET", "/posts/p101/oembed.json", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"provider_name":"OpenVerse"`) {
		t.Errorf("oembed: %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(do(t, s, "GET", "/posts/p101", nil).Body.String(), `application/json+oembed`) {
		t.Error("post pages advertise their oEmbed")
	}
	if w := do(t, s, "GET", "/topics", nil); w.Code != 404 {
		t.Errorf("topics without a tag: %d", w.Code)
	}
}

func TestAdminGoesToTheJuxtapositionUI(t *testing.T) {
	var seen []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI()+" host="+r.Host)
		w.Header().Set("Content-Security-Policy", "upstream-policy")
		_, _ = w.Write([]byte("admin ok"))
	}))
	defer up.Close()
	cfg := Config{Demo: true, SessionKey: strings.Repeat("k", 32), PublicURL: "https://verse.example", OpenPakURL: "https://openpak.org", LegacyURL: up.URL}
	s, err := New(cfg, demo.New())
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/admin/posts?x=1", nil)
	r.Host = "verse.example"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Body.String() != "admin ok" || len(seen) != 1 || seen[0] != "GET /admin/posts?x=1 host=verse.example" {
		t.Errorf("admin: %q %v", w.Body.String(), seen)
	}
	if got := w.Header().Values("Content-Security-Policy"); len(got) != 1 || got[0] != "upstream-policy" {
		t.Errorf("the admin pages keep their own policy: %v", got)
	}
	// Sign-in for the admin pages goes to the UI; an ordinary old /login goes to ours.
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/login?redirect=%2Fadmin%2Fposts", nil))
	if w.Body.String() != "admin ok" {
		t.Error("/login?redirect=/admin must reach the UI")
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/login", nil))
	if w.Header().Get("Location") != "/auth/login" {
		t.Errorf("plain /login: %q", w.Header().Get("Location"))
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/login/callback?code=1", nil))
	if w.Body.String() != "admin ok" {
		t.Error("/login/callback is the UI's")
	}
	// With no legacy UI configured there is no /admin at all.
	if w := do(t, newSite(t), "GET", "/admin", nil); w.Code != 404 {
		t.Errorf("no legacy: %d", w.Code)
	}
}

func TestCommunityBrowser(t *testing.T) {
	s := newSite(t)
	body := do(t, s, "GET", "/communities", nil).Body.String()
	for _, want := range []string{"General rooms", "All games", "Popular now", `class="pills"`, ">Switch<", ">Wii U<", ">3DS<", `role="search"`, "Announcements"} {
		if !strings.Contains(body, want) {
			t.Errorf("browser lacks %q", want)
		}
	}
	if strings.Contains(body, "No posts yet") || strings.Contains(body, `loading="lazy" width="64"`) {
		t.Error("tiles must not announce quiet games or lazy-load their art")
	}
	games := body[strings.Index(body, `id="games"`):]
	if strings.Contains(games, "Announcements") || strings.Contains(games, "Help and Questions") {
		t.Error("rooms are pinned apart from the games")
	}
	// A console filter keeps only that console's games and drops the rooms.
	sw := do(t, s, "GET", "/communities?console=Switch", nil).Body.String()
	if !strings.Contains(sw, "Mario Kart") || strings.Contains(sw, "Fire Emblem Awakening") || strings.Contains(sw, "General rooms") {
		t.Error("console filter")
	}
	// Search.
	if q := do(t, s, "GET", "/communities?q=zelda", nil).Body.String(); !strings.Contains(q, "Breath of the Wild") || strings.Contains(q, "Splatoon<") {
		t.Error("search")
	}
	if q := do(t, s, "GET", "/communities?q=nothingmatchesthis", nil).Body.String(); !strings.Contains(q, "No game found") {
		t.Error("empty search state")
	}
	// Signed in: Your communities.
	if in := do(t, s, "GET", "/communities", nil, signIn(t, s, "")).Body.String(); !strings.Contains(in, "Your communities") {
		t.Error("followed communities row")
	}
	// The composer picker is the same browser.
	if np := do(t, s, "GET", "/new", nil, signIn(t, s, "")).Body.String(); !strings.Contains(np, `role="search"`) || !strings.Contains(np, "/new?community=") {
		t.Error("composer picker")
	}
}

func TestEveryAuthorShowsTheWholeName(t *testing.T) {
	s := newSite(t)
	s.names.Put(context.Background(), "1000000005", "Mika Kobayashi-Longname")
	c := signIn(t, s, "")
	for _, p := range []string{"/posts/p101", "/communities/101", "/users/1000000005", "/notifications", "/feed?scope=everyone"} {
		if body := do(t, s, "GET", p, nil, c).Body.String(); !strings.Contains(body, "Mika Kobayashi-Longname") {
			t.Errorf("%s: whole name missing", p)
		}
	}
	// Someone with no recorded name keeps the console name.
	if body := do(t, s, "GET", "/posts/p101", nil, c).Body.String(); !strings.Contains(body, "Ren") {
		t.Error("console name kept")
	}
}

func TestFamilies(t *testing.T) {
	s := newSite(t)
	body := do(t, s, "GET", "/communities", nil).Body.String()
	games := body[strings.Index(body, `id="games"`):]
	// One tile for the franchise; its games have none of their own until searched for.
	if !strings.Contains(games, `href="/communities/201"`) || !strings.Contains(games, "5 games") {
		t.Error("Mario Kart family tile")
	}
	if strings.Contains(games, "Mario Kart DS") || strings.Contains(games, `href="/communities/103"`) {
		t.Error("a game inside a family must not have its own tile in the browser")
	}
	if !strings.Contains(games, `href="/communities/101"`) {
		t.Error("a game that stands alone keeps its tile")
	}
	// Search finds the single game and lands on the family with its chip chosen.
	q := do(t, s, "GET", "/communities?q=kart+7", nil).Body.String()
	if !strings.Contains(q, `href="/communities/201?game=111"`) || !strings.Contains(q, "Mario Kart · ") {
		t.Errorf("search for one game: %s", q[strings.Index(q, `id="games"`):])
	}
	// The family page: a chip per game, All first, every game's posts together.
	page := do(t, s, "GET", "/communities/201", nil).Body.String()
	for _, want := range []string{`class="pills games"`, ">Mario Kart 7 · 3DS<", ">Mario Kart 8 · Wii U<", ">Mario Kart 8 Deluxe · Switch<", `aria-current="page">All<`,
		"Rainbow Road", "StreetPass ghosts", "Mount Wario"} {
		if !strings.Contains(page, want) {
			t.Errorf("family page lacks %q", want)
		}
	}
	// A chip shows only that game's posts.
	c := signIn(t, s, "")
	one := do(t, s, "GET", "/communities/201?game=111", nil, c).Body.String()
	if !strings.Contains(one, "StreetPass ghosts") || strings.Contains(one, "Rainbow Road") || strings.Contains(one, "Mount Wario") {
		t.Error("chip filter")
	}
	if !strings.Contains(one, `href="/new?community=111"`) {
		t.Error("Post here follows the chosen game")
	}
	if !strings.Contains(do(t, s, "GET", "/communities/201", nil, c).Body.String(), `href="/new?community=201"`) {
		t.Error("Post here on All leads to the game picker")
	}
	// A game's old address opens its family with the chip chosen.
	w := do(t, s, "GET", "/communities/111", nil)
	if w.Code != 302 || w.Header().Get("Location") != "/communities/201?game=111" {
		t.Errorf("game address: %d %q", w.Code, w.Header().Get("Location"))
	}
	// A game that stands alone has no chip row.
	if alone := do(t, s, "GET", "/communities/101", nil).Body.String(); strings.Contains(alone, `class="pills games"`) {
		t.Error("no chips on a single game")
	}
	// The composer asks which game; a family itself takes no post.
	if np := do(t, s, "GET", "/new?community=201", nil, c).Body.String(); !strings.Contains(np, `<select id="game"`) || !strings.Contains(np, "Choose a game") {
		t.Error("composer game picker")
	}
	if np := do(t, s, "GET", "/new?community=111", nil, c).Body.String(); strings.Contains(np, `<select id="game"`) {
		t.Error("a game takes the post directly")
	}
	form := url.Values{"community": {"201"}, "body": {"hello"}}
	if w := do(t, s, "POST", "/new", form, c); w.Code != 422 || !strings.Contains(w.Body.String(), "Choose which game") {
		t.Errorf("post to a family: %d", w.Code)
	}
	form.Set("community", "111")
	if w := do(t, s, "POST", "/new", form, c); w.Code != 303 {
		t.Errorf("post to a game of a family: %d", w.Code)
	}
	// Followed games sit under their family in Your communities.
	if in := do(t, s, "GET", "/communities", nil, c).Body.String(); !strings.Contains(in, "Your communities") || !strings.Contains(in, `href="/communities/202?game=102"`) {
		t.Error("a followed game opens its family")
	}
}
