package miiverse

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openpak/openverse/internal/store"
)

// A fake bridge: records each request and answers with canned JSON.
func fake(t *testing.T, answers map[string]string) (*Client, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.RequestURI()+" pid="+r.Header.Get("X-Trusted-Pid")+" key="+r.Header.Get("X-Internal-Key")+" "+string(b))
		a, ok := answers[r.Method+" "+r.URL.Path]
		if !ok {
			http.Error(w, `{"errorCode":"not_found"}`, 404)
			return
		}
		_, _ = w.Write([]byte(a))
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "k"), &seen
}

const postJSON = `{"id":"abc","createdAt":"2026-11-01T10:00:00Z","parentId":null,"community":{"olive_community_id":"7","name":"Splatoon","iconImagePaths":{"64":"https://api.olv.openpak.org/cdn/icons/7/64.png","128":""}},
 "author":{"miiName":"Ren","pid":1000000012,"verified":false},"mii":{"imageUrl":"https://api.olv.openpak.org/cdn/mii/1000000012/normal_face.png"},"body":"hello","painting":{"imageUrl":"https://api.olv.openpak.org/cdn/paintings/1000000012/abc.png"},
 "stats":{"empathyCount":3,"replyCount":1},"yeahsBy":[{"pid":1800000001}],"isSpoiler":false}`

func TestPostMapping(t *testing.T) {
	c, seen := fake(t, map[string]string{"GET /internal/v1/web/api/v1/posts/abc": postJSON})
	p, err := c.Post(context.Background(), "1800000001", "abc")
	if err != nil {
		t.Fatal(err)
	}
	if p.Author.Avatar != "/cdn/mii/1000000012/normal_face.png" || p.Painting != "/cdn/paintings/1000000012/abc.png" || p.Community.Icon != "/cdn/icons/7/64.png" {
		t.Errorf("pictures not mapped to /cdn: %+v", p)
	}
	if !p.Yeahed || p.Yeahs != 3 || p.Replies != 1 || p.Body != "hello" {
		t.Errorf("fields: %+v", p)
	}
	if !strings.Contains((*seen)[0], "pid=1800000001 key=k") {
		t.Errorf("headers: %s", (*seen)[0])
	}
}

func TestWebOnlyPeopleHaveNoMiiImage(t *testing.T) {
	if avatarFor(1800000044, "https://x/cdn/mii/1800000044/normal_face.png") != "" {
		t.Error("web-only person should show an initial, not a missing picture")
	}
	if avatarFor(OfficialPID, "") == "" {
		t.Error("the official account has an avatar")
	}
}

func TestNullAndErrors(t *testing.T) {
	c, _ := fake(t, map[string]string{"GET /internal/v1/web/api/v1/posts/gone": `null`})
	if _, err := c.Post(context.Background(), "", "gone"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("null: %v", err)
	}
	if _, err := c.Post(context.Background(), "", "never"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("404: %v", err)
	}
	for code, want := range map[int]error{403: store.ErrForbidden, 423: store.ErrForbidden, 429: store.ErrRateLimit, 400: store.ErrRejected} {
		if err := statusError(code, []byte("x")); !errors.Is(err, want) {
			t.Errorf("%d: %v", code, err)
		}
	}
}

func TestWritesGoToTheRightRoutes(t *testing.T) {
	c, seen := fake(t, map[string]string{
		"POST /internal/v1/web/api/v1/communities/7/posts": postJSON,
		"POST /internal/v1/web/api/v1/posts/abc/replies":   postJSON,
		"POST /internal/v1/web/api/v1/posts/abc/empathies": `{"action":"add","post_id":"abc","empathy_count":4}`,
		"POST /internal/v1/web/api/v1/sso/resolve":         `{"pid":1800000001,"linked":false,"rulesAccepted":false}`,
	})
	ctx := context.Background()
	if _, err := c.CreatePost(ctx, "1800000001", "7", store.NewPost{Body: "hi", Drawing: "Qk0="}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Reply(ctx, "1800000001", "abc", store.NewPost{Body: "re"}); err != nil {
		t.Fatal(err)
	}
	n, err := c.Yeah(ctx, "1800000001", "abc", true)
	if err != nil || n != 4 {
		t.Fatalf("yeah %d %v", n, err)
	}
	id, err := c.Resolve(ctx, "acct", "Tobi", nil)
	if err != nil || id.ID != "1800000001" || id.RulesAccepted {
		t.Fatalf("resolve %+v %v", id, err)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(strings.SplitN((*seen)[0], "key=k ", 2)[1]), &body)
	p, _ := body["painting"].(map[string]any)
	if body["body"] != "hi" || p["file"] != "Qk0=" || p["isBmp"] != true {
		t.Errorf("post body: %v", body)
	}
	if !strings.Contains((*seen)[2], `{"action":"add"}`) {
		t.Errorf("empathy body: %s", (*seen)[2])
	}
}

func TestMediaKey(t *testing.T) {
	for in, want := range map[string]string{
		"https://api.olv.openpak.org/cdn/icons/1/64.png": "/cdn/icons/1/64.png",
		"/paintings/5/x.png":                             "/cdn/paintings/5/x.png",
		"https://example.org/other/thing.png":            "",
		"":                                               "",
	} {
		if got := mediaKey(in); got != want {
			t.Errorf("mediaKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCommunitiesAreReadNinetyAtATime(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/stats") {
			_, _ = w.Write([]byte(`{"totalPosts":7}`))
			return
		}
		lim, off := r.URL.Query().Get("limit"), r.URL.Query().Get("offset")
		asked = append(asked, lim+"@"+off)
		n := 90
		if off == "270" {
			n = 30
		}
		var items []string
		for i := 0; i < n; i++ {
			items = append(items, `{"olive_community_id":"c`+off+"_"+strings.Repeat("x", i%3)+string(rune('a'+i%26))+`","name":"G","followerCount":2,"iconImagePaths":{"128":"https://api/cdn/icons/1/128.png"}}`)
		}
		_, _ = w.Write([]byte(`{"total":300,"items":[` + strings.Join(items, ",") + `]}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "k")
	list, total, err := c.Communities(context.Background(), store.Page{Limit: 600})
	if err != nil || total != 300 {
		t.Fatalf("%v %d", err, total)
	}
	if strings.Join(asked, " ") != "90@ 90@90 90@180 90@270" {
		t.Errorf("requests: %v", asked)
	}
	if len(list) != 300 {
		t.Errorf("got %d communities", len(list))
	}
	for _, com := range list {
		if com.Posts != 7 {
			t.Fatalf("post count not filled in: %+v", com)
		}
	}
}

func TestConsolesComeFromTheDescription(t *testing.T) {
	for desc, want := range map[string]string{
		"Talk about Zelda on Wii. Share tips.":          "Wii",
		"Talk about X on Wii U and Switch. Share tips.": "Wii U,Switch",
		"Talk about Y on Wii and DS. Share tips.":       "Wii,DS",
		"Talk about Z on 3DS. Share tips.":              "3DS",
		"Tell us what works and what does not.":         "",
		"Everything about Switch games.":                "",
	} {
		if got := strings.Join(consolesOf(desc, ""), ","); got != want {
			t.Errorf("%q -> %q, want %q", desc, got, want)
		}
	}
}
