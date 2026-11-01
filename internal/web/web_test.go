package web

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/openpak/openverse/internal/demo"
)

func newSite(t *testing.T) *Site {
	t.Helper()
	cfg := Config{Demo: true, SessionKey: strings.Repeat("k", 32), PublicURL: "http://example.test", OpenPakURL: "https://openpak.org", ContactEmail: "c@example.test"}
	s, err := New(cfg, demo.New())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func do(t *testing.T, s *Site, method, target string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	r := httptest.NewRequest(method, target, body)
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func signIn(t *testing.T, s *Site, as string) *http.Cookie {
	t.Helper()
	w := do(t, s, "GET", "/auth/login?next=/feed"+as, nil)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("login: %d", w.Code)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == cookieName {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func TestTopBarIsTheOnlyNavigation(t *testing.T) {
	s := newSite(t)
	out := do(t, s, "GET", "/communities", nil).Body.String()
	for _, want := range []string{`class="brand"`, `>Communities<`, `>About<`, `/auth/login?next=`} {
		if !strings.Contains(out, want) {
			t.Errorf("signed out: missing %q", want)
		}
	}
	for _, no := range []string{`href="/feed"`, `href="/notifications"`, `>New post<`, `id="nav-menu"`, "sidebar", "pretendo.network"} {
		if strings.Contains(out, no) {
			t.Errorf("signed out: unexpected %q", no)
		}
	}
	in := do(t, s, "GET", "/communities", nil, signIn(t, s, "")).Body.String()
	for _, want := range []string{`href="/feed"`, `href="/activity"`, `href="/notifications"`, `data-unread`, `class="btn primary" href="/new"`, `>New post<`, `class="acct"`, `href="/settings"`, `href="/auth/logout"`, `class="tabbar"`} {
		if !strings.Contains(in, want) {
			t.Errorf("signed in: missing %q", want)
		}
	}
	if strings.Contains(in, "Messages") {
		t.Error("there is no messages page, so no messages link")
	}
}

func TestEveryPageRenders(t *testing.T) {
	s := newSite(t)
	c := signIn(t, s, "")
	for _, p := range []string{"/", "/communities", "/communities/101", "/communities/101?sort=popular", "/feed", "/feed?scope=everyone", "/activity", "/notifications", "/new",
		"/new?community=101", "/posts/p101", "/posts/p101?sort=newest", "/me", "/users/1000000031", "/settings", "/about"} {
		w := do(t, s, "GET", p, nil, c)
		if w.Code != http.StatusOK && w.Code != http.StatusSeeOther {
			t.Errorf("%s: %d", p, w.Code)
			continue
		}
		body := w.Body.String()
		if w.Code == http.StatusOK && (!strings.Contains(body, "· OpenVerse</title>") || strings.Contains(body, "<no value>") || strings.Contains(body, "%!")) {
			t.Errorf("%s: bad page", p)
		}
	}
	if w := do(t, s, "GET", "/posts/nope", nil); w.Code != http.StatusNotFound {
		t.Errorf("missing post: %d", w.Code)
	}
}

func TestGuestsAreSentToSignIn(t *testing.T) {
	s := newSite(t)
	for _, p := range []string{"/feed", "/activity", "/notifications", "/new", "/settings", "/me"} {
		w := do(t, s, "GET", p, nil)
		if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/auth/login?next=") {
			t.Errorf("%s: %d %s", p, w.Code, w.Header().Get("Location"))
		}
	}
}

func TestFirstVisitGoesToWelcomeAndAcceptWorks(t *testing.T) {
	s := newSite(t)
	// demo store starts with rules accepted; make a session that has not accepted
	c := signIn(t, s, "&as=new")
	w := do(t, s, "GET", "/feed", nil, c)
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/welcome?next=") {
		t.Fatalf("expected welcome redirect, got %d %s", w.Code, w.Header().Get("Location"))
	}
	page := do(t, s, "GET", "/welcome?next=/feed", nil, c).Body.String()
	if strings.Count(page, `<button type="submit" class="btn`) != 2 || !strings.Contains(page, "Accept and continue") || !strings.Contains(page, "Not now") {
		t.Error("welcome needs two real buttons")
	}
	w = do(t, s, "POST", "/welcome", url.Values{"next": {"/feed"}}, c)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/feed" {
		t.Fatalf("accept: %d %s", w.Code, w.Header().Get("Location"))
	}
	var c2 *http.Cookie
	for _, k := range w.Result().Cookies() {
		if k.Name == cookieName {
			c2 = k
		}
	}
	if c2 == nil || do(t, s, "GET", "/feed", nil, c2).Code != http.StatusOK {
		t.Error("after accepting, the feed opens")
	}
}

func TestPostingAndReplying(t *testing.T) {
	s := newSite(t)
	c := signIn(t, s, "")
	w := do(t, s, "POST", "/new", url.Values{"community": {"101"}, "body": {"Hello from a test"}}, c)
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/posts/") {
		t.Fatalf("post: %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if body := do(t, s, "GET", loc, nil, c).Body.String(); !strings.Contains(body, "Hello from a test") {
		t.Error("new post not shown")
	}
	w = do(t, s, "POST", loc+"/reply", url.Values{"body": {"A reply"}}, c)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("reply: %d", w.Code)
	}
	if body := do(t, s, "GET", loc, nil, c).Body.String(); !strings.Contains(body, "A reply") || !strings.Contains(body, "1 reply") {
		t.Error("reply not shown")
	}
	// empty, too long, rejected
	for name, form := range map[string]url.Values{
		"empty":    {"community": {"101"}, "body": {"   "}},
		"long":     {"community": {"101"}, "body": {strings.Repeat("a", 281)}},
		"rejected": {"community": {"101"}, "body": {"this is badword text"}},
		"bad bmp":  {"community": {"101"}, "kind": {"painting"}, "painting": {base64.StdEncoding.EncodeToString([]byte("not a bmp at all, definitely longer than sixty-two bytes of data, really"))}},
	} {
		w := do(t, s, "POST", "/new", form, c)
		if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), `role="alert"`) {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
}

func TestDrawingIsAcceptedOnlyAsBMP(t *testing.T) {
	s := newSite(t)
	c := signIn(t, s, "")
	bmp := make([]byte, 62+1920)
	bmp[0], bmp[1] = 'B', 'M'
	w := do(t, s, "POST", "/new", url.Values{"community": {"112"}, "kind": {"painting"}, "painting": {base64.StdEncoding.EncodeToString(bmp)}}, c)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("drawing post: %d", w.Code)
	}
	if body := do(t, s, "GET", w.Header().Get("Location"), nil, c).Body.String(); !strings.Contains(body, `class="painting"`) {
		t.Error("drawing not shown")
	}
}

func TestYeahJSONAndForm(t *testing.T) {
	s := newSite(t)
	c := signIn(t, s, "")
	r := httptest.NewRequest("POST", "/posts/p101/yeah", strings.NewReader("add=1"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Accept", "application/json")
	r.AddCookie(c)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || !regexp.MustCompile(`"count":\d+`).MatchString(w.Body.String()) {
		t.Fatalf("yeah json: %d %s", w.Code, w.Body.String())
	}
	if w := do(t, s, "POST", "/posts/p101/yeah", url.Values{"add": {"0"}, "back": {"/feed"}}, c); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/feed" {
		t.Errorf("yeah form: %d %s", w.Code, w.Header().Get("Location"))
	}
}

func TestCrossSiteWritesAreRefused(t *testing.T) {
	s := newSite(t)
	c := signIn(t, s, "")
	r := httptest.NewRequest("POST", "/new", strings.NewReader("community=101&body=x"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.AddCookie(c)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("cross-site: %d", w.Code)
	}
	r = httptest.NewRequest("POST", "/new", strings.NewReader("community=101&body=x"))
	r.Header.Set("Origin", "https://evil.test")
	r.AddCookie(c)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("bad origin: %d", w.Code)
	}
}

func TestLogoutIsSiteOnly(t *testing.T) {
	s := newSite(t)
	w := do(t, s, "GET", "/auth/logout", nil, signIn(t, s, ""))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/about" {
		t.Errorf("logout: %d %s", w.Code, w.Header().Get("Location"))
	}
}

func TestSessionIsTamperProof(t *testing.T) {
	s := newSite(t)
	c := signIn(t, s, "")
	bad := &http.Cookie{Name: c.Name, Value: c.Value[:len(c.Value)-2] + "xx"}
	if w := do(t, s, "GET", "/feed", nil, bad); w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/auth/login") {
		t.Errorf("tampered cookie accepted: %d", w.Code)
	}
}

func TestLinkifyEscapesAndLinks(t *testing.T) {
	got := string(linkify(`<b>hi</b> see https://example.org/a?b=1&c=2. Thanks`))
	for _, want := range []string{"&lt;b&gt;hi&lt;/b&gt;", `<a href="https://example.org/a?b=1&amp;c=2" rel="noopener nofollow ugc" target="_blank">`, "</a>. Thanks"} {
		if !strings.Contains(got, want) {
			t.Errorf("linkify: missing %q in %s", want, got)
		}
	}
}

func TestLocalPathStaysLocal(t *testing.T) {
	for in, want := range map[string]string{"/feed": "/feed", "//evil.test": "/", "https://evil.test": "/", "/auth/login": "/", "/a\r\nb": "/", "/\\evil": "/"} {
		if got := localPath(in); got != want {
			t.Errorf("localPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNoUpstreamBrandingInPages(t *testing.T) {
	s := newSite(t)
	c := signIn(t, s, "")
	for _, p := range []string{"/communities", "/feed", "/settings"} {
		body := do(t, s, "GET", p, nil, c).Body.String()
		if regexp.MustCompile(`(?i)pretendo\.|preten\.do|juxt\.`).MatchString(body) {
			t.Errorf("%s links upstream", p)
		}
	}
	about := do(t, s, "GET", "/about", nil).Body.String()
	if !strings.Contains(about, "Juxtaposition by Pretendo Network") {
		t.Error("about carries the AGPL credit")
	}
}

func TestCDNProxyOnlyServesMediaPrefixes(t *testing.T) {
	s := newSite(t)
	if w := do(t, s, "GET", "/cdn/icons/101/128.png", nil); w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "image/") {
		t.Errorf("icon: %d", w.Code)
	}
	if w := do(t, s, "GET", "/cdn/secrets/x", nil); w.Code != 404 {
		t.Errorf("other prefix: %d", w.Code)
	}
	if w := do(t, s, "GET", "/cdn/icons/../x", nil); w.Code != 404 && w.Code != 301 && w.Code != 307 {
		t.Errorf("traversal: %d", w.Code)
	}
}

func TestWelcomeShowsNoNavigation(t *testing.T) {
	s := newSite(t)
	c := signIn(t, s, "&as=new")
	page := do(t, s, "GET", "/welcome?next=/feed", nil, c).Body.String()
	for _, no := range []string{`href="/feed"`, `class="navlinks"`, `class="tabbar"`, `>New post<`} {
		if strings.Contains(page, no) {
			t.Errorf("welcome should not show %q", no)
		}
	}
}

func TestWelcomeTermsLinkIsInline(t *testing.T) {
	s := newSite(t)
	c := signIn(t, s, "&as=new")
	page := do(t, s, "GET", "/welcome?next=/feed", nil, c).Body.String()
	if strings.Contains(page, `class="terms"`) || !strings.Contains(page, "(<a href=\"https://openpak.org/terms\" rel=\"noopener\">full terms</a>) before you start") {
		t.Error("terms link must sit inside the intro sentence")
	}
}

func TestRepliesCarryOnlyYeah(t *testing.T) {
	s := newSite(t)
	c := signIn(t, s, "")
	body := do(t, s, "GET", "/posts/p101", nil, c).Body.String()
	replies := body[strings.Index(body, `class="stack replies"`):strings.Index(body, `class="card composer reply-box"`)]
	if strings.Contains(replies, "data-share") || strings.Contains(replies, "#replies") || strings.Contains(replies, "/communities/") {
		t.Error("reply cards show only the author, time and Yeah")
	}
	if !strings.Contains(replies, "act yeah") {
		t.Error("replies keep Yeah")
	}
}
