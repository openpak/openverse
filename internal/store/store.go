// Package store is the line between OpenVerse's web app and wherever the data lives.
//
// The web app only ever talks to a Store. Today the implementation is internal/miiverse, which reads
// and writes through the miiverse-api web bridge, so console posts and web posts stay one dataset.
// The rebuild plan moves storage into this service later (Postgres, keyed by the OpenPak account);
// that is a second implementation of this interface, not a change to the pages.
//
// Types here describe what a page needs, not what any backend sends: no pids as keys in URLs the
// backend invented, no console wire formats.
package store

import (
	"context"
	"errors"
	"time"
)

// Errors a Store returns. Anything else is a failure of the backend itself.
var (
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("not allowed")       // read-only (supervised) account, closed community, someone else's post
	ErrRejected  = errors.New("post rejected")     // automod or validation refused the content
	ErrRateLimit = errors.New("too many requests") // slow down
)

// Identity is a signed-in OpenPak account as OpenVerse knows it.
type Identity struct {
	Account       string // the OpenPak account id
	Name          string // display name from openpak.org
	ID            string // the number OpenVerse knows the person by (stable, used in profile URLs)
	RulesAccepted bool
}

// Viewer is what a page needs to know about the person looking: what they follow and may do.
type Viewer struct {
	ID                  string
	ReadOnly            bool // a supervised account may read but not post
	Moderator           bool
	FollowedCommunities map[string]bool
	FollowedUsers       map[string]bool
}

// Person is the author of a post or a profile.
type Person struct {
	ID       string
	Name     string
	Handle   string // @name, empty for people with no console account
	Avatar   string // a path on this site (/cdn/...), empty when there is none
	Verified bool
}

// CommunityRef is the small community shown on a post.
type CommunityRef struct {
	ID   string
	Name string
	Icon string
}

// Community is a place posts go: usually a game.
type Community struct {
	ID          string
	Name        string
	Description string
	Icon        string // 128 px
	Banner      string
	Followers   int
	Posts       int // -1 when unknown
	Open        bool
	Following   bool
	HasRelated  bool
}

// Post is a post or a reply.
type Post struct {
	ID        string
	ParentID  string
	Created   time.Time
	Author    Person
	Community CommunityRef
	Body      string
	Painting  string // a drawing, path on this site
	Image     string // a screenshot
	Topic     string
	Yeahs     int
	Replies   int
	Yeahed    bool
	Spoiler   bool
	Removed   bool
	RemovedBy string
	Official  bool // posted by the verified OpenVerse/Pakka account
	Origin    string
}

// Profile is a person's page.
type Profile struct {
	Person
	Bio       string
	Country   string
	Followers int
	Posts     int
	Following bool
	Self      bool
	Flags     []string
	Private   bool
}

// Notification is one line on the notifications page.
type Notification struct {
	Kind    string // follow, yeah, reply, system, removed, limited
	Created time.Time
	Read    bool
	Actors  []Person
	Others  int // how many more people did the same
	PostID  string
	Text    string // system and moderation notices
	Link    string
}

// ActivityItem is a game moment (a badge earned...) from the opt-in activity feed.
type ActivityItem struct {
	ID       string
	Person   Person
	Category string
	Subject  string
	Link     string
	Image    string
	Count    int
	Digest   bool
	Created  time.Time
}

// Settings are the per-person options on the settings page.
type Settings struct {
	ActivityOn         bool
	ActivityCategories []ActivityCategory
	ProfileVisibility  string // public | users_only
	ShowCountry        bool
	NotifyYeah         bool
	NotifyFollow       bool
	NotifyReply        bool
	Comment            string
}

// ActivityCategory is one switch of the activity opt-in.
type ActivityCategory struct {
	ID      string
	Enabled bool
}

// NewPost is what the composer sends.
type NewPost struct {
	Body    string
	Drawing string // base64 of a 1-bit BMP, empty for a text post
	Spoiler bool
}

// Page is a window onto a list.
type Page struct {
	Limit  int
	Offset int
}

// Store is everything the web app reads and writes.
type Store interface {
	// Identity
	Resolve(ctx context.Context, account, name string, wiiuPID *int64) (Identity, error)
	AcceptRules(ctx context.Context, viewer string) error
	Viewer(ctx context.Context, viewer string) (Viewer, error)
	UnreadNotifications(ctx context.Context, viewer string) (int, error)

	// Communities
	Communities(ctx context.Context, p Page) ([]Community, int, error)
	Community(ctx context.Context, viewer, id string) (Community, error)
	CommunityPosts(ctx context.Context, viewer, id string, popular bool, p Page) ([]Post, error)
	FollowCommunity(ctx context.Context, viewer, id string, follow bool) (int, error)

	// Feeds
	Feed(ctx context.Context, viewer, scope string, p Page) ([]Post, error) // scope: following | everyone
	Activity(ctx context.Context, viewer, scope string, p Page) ([]ActivityItem, error)

	// Posts
	Post(ctx context.Context, viewer, id string) (Post, error)
	Replies(ctx context.Context, viewer, id string, newestFirst bool) ([]Post, error)
	CreatePost(ctx context.Context, viewer, community string, n NewPost) (Post, error)
	Reply(ctx context.Context, viewer, post string, n NewPost) (Post, error)
	Yeah(ctx context.Context, viewer, post string, add bool) (int, error)
	DeletePost(ctx context.Context, viewer, post string) error
	ReportPost(ctx context.Context, viewer, post string, reason int, message string) error

	// People
	Profile(ctx context.Context, viewer, id string) (Profile, error)
	UserPosts(ctx context.Context, viewer, id string, p Page) ([]Post, error)
	FollowUser(ctx context.Context, viewer, id string, follow bool) (int, error)

	// Notifications and settings
	Notifications(ctx context.Context, viewer string, p Page) ([]Notification, int, error)
	Settings(ctx context.Context, viewer string) (Settings, error)
	SaveSettings(ctx context.Context, viewer string, s Settings) error
}
