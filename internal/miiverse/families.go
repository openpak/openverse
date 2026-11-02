package miiverse

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/openpak/openverse/internal/store"
)

// A family is a franchise page (Mario Kart) over its games. The API keeps it as a Sub community that
// the games name as their parent; it has no posts of its own and its feed is the games' together.
// The games themselves stay ordinary communities, so a console never meets a family.

var consoleRank = []string{"Switch", "Wii U", "3DS", "Wii", "DS", "PS3", "Xbox 360", "Xbox", "PS2", "PSP", "Vita"}

func unionConsoles(games []store.Community) []string {
	have := map[string]bool{}
	for _, g := range games {
		for _, k := range g.Consoles {
			have[k] = true
		}
	}
	var out []string
	for _, k := range consoleRank {
		if have[k] {
			out = append(out, k)
			delete(have, k)
		}
	}
	var rest []string
	for k := range have {
		rest = append(rest, k)
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// families reads every family page (the API's Sub communities that have no parent themselves).
func (c *Client) families(ctx context.Context) (map[string]wireCommunity, error) {
	out := map[string]wireCommunity{}
	for off := 0; ; {
		var page wireList[wireCommunity]
		if err := c.do(ctx, http.MethodGet, "/communities"+q(store.Page{Limit: 90, Offset: off}, url.Values{"category": {"sub"}}), "", nil, &page); err != nil {
			return nil, err
		}
		for _, w := range page.Items {
			if w.ParentID == nil {
				out[w.ID] = w
			}
		}
		off += len(page.Items)
		if len(page.Items) < 90 || off >= page.Total {
			return out, nil
		}
	}
}

// groupFamilies adds the family pages to a list of games and rooms, and tells each game its family.
// A game whose family cannot be found is set free, so a half-applied change never hides a game.
func (c *Client) groupFamilies(ctx context.Context, list []store.Community) []store.Community {
	members := map[string][]int{}
	var order []string
	for i, g := range list {
		if g.Parent != "" {
			if _, ok := members[g.Parent]; !ok {
				order = append(order, g.Parent)
			}
			members[g.Parent] = append(members[g.Parent], i)
		}
	}
	if len(order) == 0 {
		return list
	}
	docs, err := c.families(ctx)
	if err != nil {
		docs = nil
	}
	for _, id := range order {
		doc, ok := docs[id]
		if !ok {
			for _, i := range members[id] {
				list[i].Parent = ""
			}
			continue
		}
		fam := doc.toStore()
		fam.Parent, fam.General, fam.Family = "", false, true
		posts := -1
		for _, i := range members[id] {
			list[i].ParentName = fam.Name
			if list[i].Posts >= 0 {
				if posts < 0 {
					posts = 0
				}
				posts += list[i].Posts
			}
			fam.Members = append(fam.Members, list[i])
		}
		sort.SliceStable(fam.Members, func(a, b int) bool {
			return strings.ToLower(fam.Members[a].Name) < strings.ToLower(fam.Members[b].Name)
		})
		fam.Consoles, fam.Posts = unionConsoles(fam.Members), posts
		list = append(list, fam)
	}
	return list
}

// familyOf finds a family in the cached list.
func (c *Client) familyOf(ctx context.Context, id string) (store.Community, bool) {
	all, _, err := c.Communities(ctx, store.Page{Limit: 600})
	if err != nil {
		return store.Community{}, false
	}
	for _, x := range all {
		if x.ID == id && x.Family {
			return x, true
		}
	}
	return store.Community{}, false
}
