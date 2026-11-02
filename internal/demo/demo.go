// Package demo is an in-memory store.Store with made-up communities, people and posts. It runs the
// site for screenshots, local development and tests without a miiverse-api. It is never the
// production store: cmd/openverse only uses it when OPENVERSE_DEMO=1.
package demo

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/openpak/openverse/internal/store"
)

// Me is the signed-in demo person.
const Me = "1800000001"

type person struct {
	id, name, handle string
	verified, noMii  bool
}

// Store is the fixture store.
type Store struct {
	mu          sync.Mutex
	communities []store.Community
	people      map[string]person
	posts       []store.Post // oldest first
	yeahs       map[string]map[string]bool
	followedC   map[string]bool
	followedU   map[string]bool
	settings    store.Settings
	rulesOK     bool
	next        int
	now         func() time.Time
	devices     []store.DeviceLink
	// Mode makes the signed-in person supervised: "" full access, "read" or "off" (tests and screenshots).
	Mode string
}

var _ store.Store = (*Store)(nil)

// New returns the fixture store.
func New() *Store {
	s := &Store{people: map[string]person{}, yeahs: map[string]map[string]bool{}, followedC: map[string]bool{"101": true, "102": true},
		followedU: map[string]bool{"2000000000": true, "1000000005": true}, rulesOK: true, next: 100, now: time.Now}
	s.seed()
	return s
}

func (s *Store) seed() {
	for _, p := range []person{
		{id: Me, name: "Tobi", noMii: true},
		{id: "2000000000", name: "Pakka", handle: "pakka", verified: true},
		{id: "1000000005", name: "Mika", handle: "mika_k"},
		{id: "1000000012", name: "Ren", handle: "ren64"},
		{id: "1000000031", name: "Sol", handle: "sol_draws"},
		{id: "1800000044", name: "Ada", noMii: true},
		{id: "1000000058", name: "Hugo", handle: "hugo_p"},
	} {
		s.people[p.id] = p
	}
	com := func(id, name, desc string, followers, posts int) {
		s.communities = append(s.communities, store.Community{ID: id, Name: name, Description: desc, Followers: followers, Posts: posts,
			Icon: "/cdn/icons/" + id + "/128.png", Banner: "/cdn/headers/" + id + "/wup.png", Open: true})
	}
	com("101", "Super Mario Maker", "Share courses, tips and clears. Post your course ID and tell people what to look out for.", 4210, 1830)
	com("102", "Splatoon", "Turf wars, Splatfest chatter and loadout talk.", 3560, 2204)
	com("103", "Mario Kart 8 Deluxe", "Time trials, ghosts, tournaments and favourite shortcuts.", 2980, 1491)
	com("104", "Animal Crossing: New Horizons", "Island tours, villagers and turnip prices.", 5120, 3012)
	com("105", "The Legend of Zelda: Breath of the Wild", "Shrines, korok seeds and glitches you can show off.", 3890, 1655)
	com("106", "Super Smash Bros. for Wii U", "Matchups, tier talk and set-up guides.", 1740, 902)
	com("107", "Pokémon Sun and Moon", "Trades, battles and Alola team ideas.", 2210, 1310)
	com("108", "Xenoblade Chronicles", "Spoiler-tagged discussion welcome.", 640, 288)
	com("109", "Kirby and the Forgotten Land", "Rescue the Waddle Dees and share your copy abilities.", 1120, 540)
	com("110", "Fire Emblem Awakening", "Pair-ups, supports and classic mode stories.", 980, 430)
	com("111", "Mario Kart 7", "Community races and 3DS StreetPass tags.", 870, 395)
	com("112", "Drawing and Art", "Show what you drew. 320 by 120 pixels of pure imagination.", 2760, 4410)
	com("113", "Announcements", "News from the OpenPak team.", 6020, 77)
	com("114", "Help and Questions", "Ask how things work. Friendly answers only.", 1330, 802)

	ago := func(h float64) time.Time { return s.now().Add(-time.Duration(h * float64(time.Hour))) }
	add := func(com, who, body string, h float64, yeahs, replies int, opts ...string) string {
		s.next++
		id := "p" + strconv.Itoa(s.next)
		c := s.com(com)
		p := store.Post{ID: id, Created: ago(h), Author: s.person(who), Community: store.CommunityRef{ID: c.ID, Name: c.Name, Icon: c.Icon},
			Body: body, Yeahs: yeahs, Replies: replies, Origin: "web"}
		for _, o := range opts {
			switch o {
			case "paint":
				p.Painting = "/cdn/paintings/" + who + "/" + id + ".png"
			case "spoiler":
				p.Spoiler = true
			case "wiiu":
				p.Origin = "wiiu"
			}
		}
		s.posts = append(s.posts, p)
		return id
	}
	r1 := add("101", "1000000005", "Finally cleared my own Kaizo-lite course without a single checkpoint. The trick is to stop trying to jump on the second Koopa. Course ID is in the thread.", 30, 41, 4, "wiiu")
	add("112", "1000000031", "Rainy day sketch. The cat is not impressed.", 26, 63, 2, "paint")
	add("102", "1000000012", "Anyone else running the splat charger with a 55 in the Tower Control rotation? It feels so much better than I expected.", 21, 18, 3)
	add("104", "1800000044", "Turnips are at 612 bells on my island right now. DM me if you want a visit, I will keep the gate open for the next hour!", 8, 27, 0)
	add("113", "2000000000", "Welcome to the new OpenVerse. Same conversations as on your Wii U and 3DS, now with a place on the web too. Say hello in Help and Questions if you get stuck.", 50, 120, 6)
	add("101", Me, "Anyone have good tips for the water world in a one-hit-point course? I keep dying at the same spot.", 4.5, 9, 3)
	add("112", "1000000031", "Little robot, big dreams.", 3, 38, 1, "paint")
	add("103", "1000000058", "New personal best on Rainbow Road, 1:52.4. The ghost upload worked first try.", 2.2, 12, 0)
	add("105", "1000000012", "Found the hidden shrine near the old fort. I won't say where, but it involves a very patient chicken.", 1.4, 22, 1, "spoiler")
	add("102", Me, "Hi everyone, first post here. Looking for a Splatfest team to join this weekend.", 0.6, 5, 1)

	reply := func(parent, who, body string, h float64) {
		s.next++
		pp := s.get(parent)
		s.posts = append(s.posts, store.Post{ID: "p" + strconv.Itoa(s.next), ParentID: parent, Created: ago(h), Author: s.person(who),
			Community: pp.Community, Body: body, Yeahs: 1 + s.next%5})
	}
	reply(r1, "1000000012", "Congrats! That Koopa part got me too. What is your course ID?", 29)
	reply(r1, "1000000005", "It is in the first line of the post, check the text again.", 28.5)
	reply(r1, "1800000044", "Saving this one for the weekend.", 20)
	reply(r1, Me, "Just tried it. The second Koopa trick works!", 5)
	s.yeahs["p"+strconv.Itoa(105)] = map[string]bool{Me: true}
	t := s.now()
	used := t.Add(-26 * time.Hour)
	s.devices = []store.DeviceLink{
		{ID: "d1", Class: "ps3", Label: "PlayStation 3", Created: t.Add(-9 * 24 * time.Hour), LastUsed: &used},
		{ID: "d2", Class: "x360", Label: "Xbox 360", Created: t.Add(-2 * 24 * time.Hour)},
	}
	s.settings = store.Settings{ActivityOn: false, ProfileVisibility: "public", ShowCountry: true, NotifyYeah: true, NotifyFollow: true, NotifyReply: true,
		Comment: "Plays everything with a pen and a grin.", ActivityCategories: []store.ActivityCategory{{ID: "badge", Enabled: true}, {ID: "splatfest"}, {ID: "kart"}, {ID: "maker", Enabled: true}}}
}

func (s *Store) com(id string) store.Community {
	for _, c := range s.communities {
		if c.ID == id {
			return c
		}
	}
	return store.Community{ID: id}
}

func (s *Store) person(id string) store.Person {
	p, ok := s.people[id]
	if !ok {
		return store.Person{ID: id, Name: "Player " + id}
	}
	out := store.Person{ID: p.id, Name: p.name, Handle: p.handle, Verified: p.verified}
	if !p.noMii {
		out.Avatar = "/cdn/mii/" + p.id + "/normal_face.png"
	}
	return out
}

func (s *Store) get(id string) store.Post {
	for _, p := range s.posts {
		if p.ID == id {
			return p
		}
	}
	return store.Post{}
}

func (s *Store) view(p store.Post, viewer string) store.Post {
	p.Yeahed = viewer != "" && s.yeahs[p.ID][viewer]
	if p.Yeahed && p.Yeahs == 0 {
		p.Yeahs = 1
	}
	n := 0
	for _, r := range s.posts {
		if r.ParentID == p.ID {
			n++
		}
	}
	if p.ParentID == "" && n > p.Replies {
		p.Replies = n
	}
	return p
}

func window[T any](in []T, p store.Page) []T {
	if p.Offset >= len(in) {
		return nil
	}
	end := len(in)
	if p.Limit > 0 && p.Offset+p.Limit < end {
		end = p.Offset + p.Limit
	}
	return in[p.Offset:end]
}

func (s *Store) topLevel(filter func(store.Post) bool, viewer string, popular bool, p store.Page) []store.Post {
	var out []store.Post
	for _, post := range s.posts {
		if post.ParentID == "" && filter(post) {
			out = append(out, s.view(post, viewer))
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if popular {
			return out[i].Yeahs > out[j].Yeahs
		}
		return out[i].Created.After(out[j].Created)
	})
	return window(out, p)
}

// ---- store.Store ------------------------------------------------------------------------------

func (s *Store) Resolve(_ context.Context, account, name string, _ *int64) (store.Identity, error) {
	return store.Identity{Account: account, Name: name, ID: Me, RulesAccepted: s.rulesOK}, nil
}
func (s *Store) AcceptRules(context.Context, string) error {
	s.mu.Lock()
	s.rulesOK = true
	s.mu.Unlock()
	return nil
}
func (s *Store) Viewer(_ context.Context, v string) (store.Viewer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return store.Viewer{ID: v, ReadOnly: s.Mode != "", Off: s.Mode == "off", FollowedCommunities: s.followedC, FollowedUsers: s.followedU}, nil
}
func (s *Store) UnreadNotifications(context.Context, string) (int, error) { return 3, nil }

func (s *Store) Communities(_ context.Context, p store.Page) ([]store.Community, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return window(append([]store.Community(nil), s.communities...), p), len(s.communities), nil
}
func (s *Store) Community(_ context.Context, viewer, id string) (store.Community, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.communities {
		if c.ID == id {
			c.Following = viewer != "" && s.followedC[id]
			return c, nil
		}
	}
	return store.Community{}, store.ErrNotFound
}
func (s *Store) CommunityPosts(_ context.Context, viewer, id string, popular bool, p store.Page) ([]store.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.topLevel(func(x store.Post) bool { return x.Community.ID == id }, viewer, popular, p), nil
}
func (s *Store) FollowCommunity(_ context.Context, _, id string, follow bool) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followedC[id] = follow
	return s.com(id).Followers, nil
}
func (s *Store) Feed(_ context.Context, viewer, scope string, p store.Page) ([]store.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.topLevel(func(x store.Post) bool {
		return scope == "everyone" || s.followedC[x.Community.ID] || s.followedU[x.Author.ID]
	}, viewer, false, p), nil
}
func (s *Store) Activity(_ context.Context, _, scope string, p store.Page) ([]store.ActivityItem, error) {
	now := s.now()
	items := []store.ActivityItem{
		{ID: "1", Person: s.person("1000000005"), Category: "badge", Subject: "earned the Course Creator badge", Created: now.Add(-2 * time.Hour), Image: "/cdn/icons/101/64.png"},
		{ID: "2", Person: s.person("1000000012"), Category: "splatfest", Subject: "finished a Splatfest in the top 10% of their team", Created: now.Add(-5 * time.Hour)},
		{ID: "3", Person: s.person("1000000058"), Category: "kart", Subject: "set a new time on Rainbow Road: 1:52.4", Created: now.Add(-9 * time.Hour), Image: "/cdn/icons/103/64.png"},
		{ID: "4", Person: s.person("1000000031"), Category: "maker", Subject: "uploaded a new course: Robot Rain", Created: now.Add(-26 * time.Hour)},
		{ID: "5", Person: s.person("1800000044"), Category: "badge", Subject: "earned 3 badges", Digest: true, Count: 3, Created: now.Add(-30 * time.Hour)},
	}
	if scope == "following" {
		items = items[:3]
	}
	return window(items, p), nil
}
func (s *Store) Post(_ context.Context, viewer, id string) (store.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.posts {
		if p.ID == id {
			return s.view(p, viewer), nil
		}
	}
	return store.Post{}, store.ErrNotFound
}
func (s *Store) Replies(_ context.Context, viewer, id string, newest bool) ([]store.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Post
	for _, p := range s.posts {
		if p.ParentID == id {
			out = append(out, s.view(p, viewer))
		}
	}
	if newest {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return out, nil
}

func (s *Store) add(viewer, community, parent string, n store.NewPost) store.Post {
	s.next++
	id := "p" + strconv.Itoa(s.next)
	c := s.com(community)
	p := store.Post{ID: id, ParentID: parent, Created: s.now(), Author: s.person(viewer), Community: store.CommunityRef{ID: c.ID, Name: c.Name, Icon: c.Icon},
		Body: n.Body, Spoiler: n.Spoiler, Origin: "web"}
	if n.Drawing != "" {
		p.Painting = "/cdn/paintings/" + viewer + "/" + id + ".png"
	}
	s.posts = append(s.posts, p)
	return p
}
func (s *Store) CreatePost(_ context.Context, viewer, community string, n store.NewPost) (store.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.com(community).Name == "" {
		return store.Post{}, store.ErrNotFound
	}
	if strings.Contains(strings.ToLower(n.Body), "badword") {
		return store.Post{}, store.ErrRejected
	}
	return s.add(viewer, community, "", n), nil
}
func (s *Store) Reply(_ context.Context, viewer, post string, n store.NewPost) (store.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	parent := s.get(post)
	if parent.ID == "" {
		return store.Post{}, store.ErrNotFound
	}
	return s.add(viewer, parent.Community.ID, post, n), nil
}
func (s *Store) Yeah(_ context.Context, viewer, post string, add bool) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.posts {
		if s.posts[i].ID == post {
			if s.yeahs[post] == nil {
				s.yeahs[post] = map[string]bool{}
			}
			if add && !s.yeahs[post][viewer] {
				s.posts[i].Yeahs++
			} else if !add && s.yeahs[post][viewer] {
				s.posts[i].Yeahs--
			}
			s.yeahs[post][viewer] = add
			return s.posts[i].Yeahs, nil
		}
	}
	return 0, store.ErrNotFound
}
func (s *Store) DeletePost(_ context.Context, viewer, post string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.posts {
		if p.ID == post {
			if p.Author.ID != viewer {
				return store.ErrForbidden
			}
			s.posts = append(s.posts[:i], s.posts[i+1:]...)
			return nil
		}
	}
	return store.ErrNotFound
}
func (s *Store) ReportPost(context.Context, string, string, int, string) error { return nil }

func (s *Store) Profile(_ context.Context, viewer, id string) (store.Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.people[id]
	if !ok {
		return store.Profile{}, store.ErrNotFound
	}
	n := 0
	for _, x := range s.posts {
		if x.Author.ID == id {
			n++
		}
	}
	pr := store.Profile{Person: s.person(id), Posts: n, Followers: 12 + len(p.name)*7, Self: viewer == id, Following: s.followedU[id], Country: "Portugal"}
	if id == Me {
		pr.Bio, pr.Followers = s.settings.Comment, 18
	}
	return pr, nil
}
func (s *Store) UserPosts(_ context.Context, viewer, id string, p store.Page) ([]store.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.topLevel(func(x store.Post) bool { return x.Author.ID == id }, viewer, false, p), nil
}
func (s *Store) FollowUser(_ context.Context, _, id string, follow bool) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followedU[id] = follow
	return 0, nil
}
func (s *Store) Notifications(_ context.Context, _ string, p store.Page) ([]store.Notification, int, error) {
	now := s.now()
	list := []store.Notification{
		{Kind: "yeah", Created: now.Add(-20 * time.Minute), PostID: "p110", Actors: []store.Person{s.person("1000000005"), s.person("1000000012")}, Others: 3},
		{Kind: "reply", Created: now.Add(-3 * time.Hour), PostID: "p106", Actors: []store.Person{s.person("1000000058")}},
		{Kind: "follow", Created: now.Add(-7 * time.Hour), Actors: []store.Person{s.person("1000000031")}, Read: true},
		{Kind: "system", Created: now.Add(-30 * time.Hour), Text: "OpenVerse now has game activity: turn it on in Settings.", Link: "/settings#activity", Read: true},
	}
	return window(list, p), len(list), nil
}
func (s *Store) Settings(context.Context, string) (store.Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings, nil
}
func (s *Store) SaveSettings(_ context.Context, _ string, in store.Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings = in
	return nil
}

// ---- pictures ---------------------------------------------------------------------------------

func hue(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() % 360)
}

// Media draws the fixture pictures as SVG: community icons and banners, avatars and the drawings.
func (s *Store) Media(_ context.Context, key string) (string, []byte, error) {
	parts := strings.Split(key, "/")
	if len(parts) < 2 {
		return "", nil, store.ErrNotFound
	}
	var svg string
	switch parts[0] {
	case "icons":
		c := s.com(parts[1])
		if c.Name == "" {
			return "", nil, store.ErrNotFound
		}
		h := hue(parts[1])
		svg = fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 128 128"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="hsl(%d 70%% 60%%)"/><stop offset="1" stop-color="hsl(%d 65%% 38%%)"/></linearGradient></defs><rect width="128" height="128" rx="28" fill="url(#g)"/><text x="64" y="84" font-family="sans-serif" font-size="64" font-weight="800" text-anchor="middle" fill="#fff" fill-opacity=".95">%s</text></svg>`,
			h, (h+40)%360, string([]rune(c.Name)[0]))
	case "headers":
		h := hue(parts[1])
		svg = fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1000 200" preserveAspectRatio="xMidYMid slice"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="hsl(%d 65%% 52%%)"/><stop offset="1" stop-color="hsl(%d 70%% 30%%)"/></linearGradient></defs><rect width="1000" height="200" fill="url(#g)"/><circle cx="820" cy="40" r="150" fill="#fff" fill-opacity=".08"/><circle cx="930" cy="170" r="110" fill="#fff" fill-opacity=".07"/><circle cx="140" cy="190" r="120" fill="#fff" fill-opacity=".06"/></svg>`, h, (h+50)%360)
	case "mii":
		p := s.person(parts[1])
		h := hue(p.ID)
		svg = fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 96 96"><rect width="96" height="96" fill="hsl(%d 55%% 82%%)"/><ellipse cx="48" cy="52" rx="25" ry="28" fill="#f7d7b5"/><path d="M22 46c0-22 14-30 26-30s26 8 26 30c-6-10-14-14-26-14s-20 4-26 14z" fill="hsl(%d 40%% 28%%)"/><circle cx="38" cy="54" r="3.2" fill="#2a2a2a"/><circle cx="58" cy="54" r="3.2" fill="#2a2a2a"/><path d="M39 68q9 8 18 0" stroke="#7a3b2a" stroke-width="3" fill="none" stroke-linecap="round"/></svg>`, h, (h+200)%360)
	case "paintings":
		svg = drawing(parts[len(parts)-1])
	default:
		return "", nil, store.ErrNotFound
	}
	return "image/svg+xml", []byte(svg), nil
}

func drawing(name string) string {
	body := `<path d="M30 92c18-40 44-60 62-20s34 36 52-10 30-38 50-6" /><path d="M200 30l6 14 15 1-11 10 4 15-14-8-14 8 4-15-11-10 15-1z"/><circle cx="250" cy="82" r="16"/><path d="M243 78h.1M257 78h.1M243 88q7 6 14 0"/><path d="M60 30q10-14 22 0"/>`
	if strings.Contains(name, "robot") || len(name)%2 == 0 {
		body = `<rect x="110" y="30" width="100" height="60" rx="8"/><path d="M160 30V16m0 0h.1M130 52h.1M190 52h.1M135 74h50M110 60H96M210 60h14M130 90v12m60-12v12"/><path d="M20 108c40-10 80 6 120 0s120-8 160 0"/>`
	}
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 320 120"><rect width="320" height="120" fill="#fff"/><g fill="none" stroke="#111" stroke-width="3" stroke-linecap="round" stroke-linejoin="round">` + body + `</g></svg>`
}
