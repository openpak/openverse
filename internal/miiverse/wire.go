package miiverse

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/openpak/openverse/internal/store"
)

// The JSON the internal API sends, only the fields the web app reads.
// Source of truth: apps/miiverse-api/internal.openapi.json.

type wireIcons struct {
	S64  string `json:"64"`
	S128 string `json:"128"`
}

type wireCommunity struct {
	ID          string    `json:"olive_community_id"`
	ParentID    *string   `json:"parentId"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Followers   int       `json:"followerCount"`
	Icons       wireIcons `json:"iconImagePaths"`
	Header      string    `json:"wupHeaderImagePath"`
	Perm        struct {
		Open bool `json:"open"`
	} `json:"permissions"`
}

type wirePost struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	ParentID  *string   `json:"parentId"`
	Community *struct {
		ID    string    `json:"olive_community_id"`
		Name  string    `json:"name"`
		Icons wireIcons `json:"iconImagePaths"`
	} `json:"community"`
	Author struct {
		Name     string `json:"miiName"`
		PID      int64  `json:"pid"`
		Verified bool   `json:"verified"`
	} `json:"author"`
	Mii struct {
		ImageURL string `json:"imageUrl"`
	} `json:"mii"`
	Body     *string `json:"body"`
	Painting *struct {
		ImageURL string `json:"imageUrl"`
	} `json:"painting"`
	Screenshot *struct {
		ImageURL string `json:"imageUrl"`
	} `json:"screenshot"`
	Stats struct {
		Yeahs   int `json:"empathyCount"`
		Replies int `json:"replyCount"`
	} `json:"stats"`
	YeahsBy    []struct{ PID int64 } `json:"yeahsBy"`
	TopicTag   *string               `json:"topicTag"`
	IsSpoiler  bool                  `json:"isSpoiler"`
	Moderation *struct {
		Removed *struct {
			RemovedBy *struct {
				PID int64 `json:"pid"`
			} `json:"removedBy"`
		} `json:"removed"`
	} `json:"moderation"`
}

type wireList[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}

// OfficialPID is the verified OpenVerse/Pakka account (miiverse-api web-identity OFFICIAL_PID).
const OfficialPID = 2000000000

// mediaKey turns the API's picture addresses into a path on this site. The API answers with full
// URLs on its own (console-only) host; browsers read the same files through our /cdn/ proxy.
func mediaKey(u string) string {
	if u == "" {
		return ""
	}
	path := u
	if p, err := url.Parse(u); err == nil && p.Path != "" {
		path = p.Path
	}
	path = strings.TrimPrefix(path, "/cdn")
	path = strings.TrimPrefix(path, "/")
	for _, pre := range []string{"paintings/", "screenshots/", "icons/", "headers/", "mii/"} {
		if strings.HasPrefix(path, pre) {
			return "/cdn/" + path
		}
		if i := strings.Index(path, "/"+pre); i >= 0 {
			return "/cdn/" + path[i+1:]
		}
	}
	return ""
}

func pidStr(p int64) string { return strconv.FormatInt(p, 10) }

// webOnlyPID is the first number given to a person with no Wii U (nn-account never issues one that high).
const webOnlyPID = 1800000000

// avatarFor is the Mii face of a console player; a web-only person has none, so the page draws their initial.
func avatarFor(pid int64, given string) string {
	if pid >= webOnlyPID && pid != OfficialPID {
		return ""
	}
	if k := mediaKey(given); k != "" {
		return k
	}
	return "/cdn/mii/" + pidStr(pid) + "/normal_face.png"
}

func (w wireCommunity) toStore() store.Community {
	return store.Community{ID: w.ID, Name: w.Name, Description: w.Description, Followers: w.Followers, Posts: -1,
		Icon: mediaKey(w.Icons.S128), Banner: mediaKey(w.Header), Open: w.Perm.Open}
}

func (w wirePost) toStore(viewer string) store.Post {
	p := store.Post{ID: w.ID, Created: w.CreatedAt, Yeahs: w.Stats.Yeahs, Replies: w.Stats.Replies, Spoiler: w.IsSpoiler,
		Author:   store.Person{ID: pidStr(w.Author.PID), Name: w.Author.Name, Verified: w.Author.Verified, Avatar: avatarFor(w.Author.PID, w.Mii.ImageURL)},
		Official: w.Author.PID == OfficialPID}
	if w.Body != nil {
		p.Body = *w.Body
	}
	if w.ParentID != nil {
		p.ParentID = *w.ParentID
	}
	if w.Community != nil {
		p.Community = store.CommunityRef{ID: w.Community.ID, Name: w.Community.Name, Icon: mediaKey(w.Community.Icons.S64)}
	}
	if w.Painting != nil && w.Painting.ImageURL != "" {
		p.Painting = mediaKey(w.Painting.ImageURL)
	}
	if w.Screenshot != nil && w.Screenshot.ImageURL != "" {
		p.Image = mediaKey(w.Screenshot.ImageURL)
	}
	if w.TopicTag != nil {
		p.Topic = *w.TopicTag
	}
	if w.Moderation != nil && w.Moderation.Removed != nil {
		p.Removed = true
		p.Body, p.Painting, p.Image = "", "", ""
	}
	for _, y := range w.YeahsBy {
		if viewer != "" && pidStr(y.PID) == viewer {
			p.Yeahed = true
		}
	}
	return p
}
