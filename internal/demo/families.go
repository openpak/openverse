package demo

import (
	"sort"
	"strings"

	"github.com/openpak/openverse/internal/store"
)

// families adds franchise pages to the fixture: Mario Kart (5 games over several consoles), Splatoon
// (3, one of them 102 above), a long Pokémon family that makes the chip row wrap, and a long family
// name. Mario Kart 8 Deluxe (103) and Mario Kart 7 (111) from the list above join the first.
func (s *Store) families(com func(id, name, desc string, followers, posts int)) {
	game := func(id, name string, followers, posts int, consoles ...string) {
		com(id, name, "Talk about "+name+" on "+strings.Join(consoles, " and ")+". Share tips, drawings and finds with other players.", followers, posts)
		for i := range s.communities {
			if s.communities[i].ID == id {
				s.communities[i].Consoles, s.communities[i].General = consoles, false
			}
		}
	}
	game("115", "Mario Kart 8", 1500, 640, "Wii U")
	game("116", "Mario Kart Wii", 900, 410, "Wii")
	game("117", "Mario Kart DS", 520, 150, "DS")
	game("118", "Splatoon 2", 2100, 1200, "Switch")
	game("119", "Splatoon 3", 2600, 1500, "Switch")
	pk := []string{"Pokémon Scarlet / Violet", "Pokémon Legends: Arceus", "Pokémon Omega Ruby / Alpha Sapphire", "Pokémon X / Y", "Pokémon Black 2 / White 2",
		"Pokémon Black / White", "Pokémon Platinum", "Pokémon HeartGold / SoulSilver", "Pokémon Diamond / Pearl", "Pokémon Battle Revolution"}
	var pkIDs []string
	for i, n := range pk {
		id := "13" + string(rune('0'+i))
		c := "Switch"
		switch {
		case i >= 2 && i < 4:
			c = "3DS"
		case i >= 4 && i < 9:
			c = "DS"
		case i == 9:
			c = "Wii"
		}
		game(id, n, 300+i*40, 90+i*20, c)
		pkIDs = append(pkIDs, id)
	}
	pkIDs = append(pkIDs, "107")
	game("140", "Winning Eleven Play Maker 2012", 30, 8, "Wii")
	game("141", "Pro Evolution Soccer 2011", 60, 14, "Wii")

	s.family("201", "Mario Kart", "Every Mario Kart game in one place. Pick a game to see its posts, or read them all together.", "103", "115", "111", "116", "117")
	s.family("202", "Splatoon", "Every Splatoon game in one place. Pick a game to see its posts, or read them all together.", "102", "118", "119")
	s.family("203", "Pokémon", "Every Pokémon game in one place. Pick a game to see its posts, or read them all together.", pkIDs...)
	s.family("204", "Pro Evolution Soccer and Winning Eleven", "Every Pro Evolution Soccer and Winning Eleven game in one place. Pick a game to see its posts, or read them all together.", "140", "141")
}

func (s *Store) family(id, name, desc string, members ...string) {
	f := store.Community{ID: id, Name: name, Description: desc, Icon: "/cdn/icons/" + members[0] + "/128.png", Banner: "/cdn/headers/" + members[0] + "/wup.png",
		Open: true, Family: true, Posts: -1}
	posts, followed := 0, 0
	for _, m := range members {
		for i := range s.communities {
			if s.communities[i].ID == m {
				s.communities[i].Parent, s.communities[i].ParentName = id, name
				f.Members = append(f.Members, s.communities[i])
				posts += s.communities[i].Posts
				followed += s.communities[i].Followers
			}
		}
	}
	sort.SliceStable(f.Members, func(a, b int) bool { return strings.ToLower(f.Members[a].Name) < strings.ToLower(f.Members[b].Name) })
	have := map[string]bool{}
	for _, m := range f.Members {
		for _, c := range m.Consoles {
			have[c] = true
		}
	}
	for _, c := range []string{"Switch", "Wii U", "3DS", "Wii", "DS"} {
		if have[c] {
			f.Consoles = append(f.Consoles, c)
		}
	}
	f.Posts, f.Followers = posts, followed/4
	s.communities = append(s.communities, f)
}

// memberIDs is the ids a family's feed reads from; for any other community, just itself.
func (s *Store) memberIDs(id string) map[string]bool {
	for _, c := range s.communities {
		if c.ID == id && c.Family {
			out := map[string]bool{}
			for _, m := range c.Members {
				out[m.ID] = true
			}
			return out
		}
	}
	return map[string]bool{id: true}
}
