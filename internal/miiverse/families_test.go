package miiverse

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openpak/openverse/internal/store"
)

func game(id, name, on, parent string) string {
	p := "null"
	if parent != "" {
		p = `"` + parent + `"`
	}
	return `{"olive_community_id":"` + id + `","parentId":` + p + `,"name":"` + name + `","description":"Talk about ` + name + ` on ` + on + `. Share tips.","followerCount":1,"iconImagePaths":{"128":"/cdn/icons/` + id + `/128.png"},"permissions":{"open":true}}`
}

func familyServer(t *testing.T, list string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/internal/v1/web/api/v1/communities" && r.URL.Query().Get("category") == "listed":
			_, _ = w.Write([]byte(list))
		case r.URL.Path == "/internal/v1/web/api/v1/communities" && r.URL.Query().Get("category") == "sub":
			_, _ = w.Write([]byte(`{"total":1,"items":[{"olive_community_id":"F","parentId":null,"name":"Mario Kart","description":"Every Mario Kart game in one place.","followerCount":3,"iconImagePaths":{"128":"/cdn/icons/8/128.png"},"permissions":{"open":true}}]}`))
		default:
			http.Error(w, `{"errorCode":"not_found"}`, 404)
		}
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "k")
}

func TestFamiliesGroupTheirGames(t *testing.T) {
	c := familyServer(t, `{"total":4,"items":[`+game("7", "Mario Kart 7", "3DS", "F")+`,`+game("8", "Mario Kart 8 Deluxe", "Switch", "F")+`,`+game("9", "Terraria", "Wii U and Switch", "")+`,`+game("r", "Help", "", "")+`]}`)
	list, _, err := c.Communities(context.Background(), store.Page{Limit: 600})
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]store.Community{}
	for _, x := range list {
		by[x.ID] = x
	}
	f := by["F"]
	if !f.Family || f.General || len(f.Members) != 2 || f.Members[0].Name != "Mario Kart 7" || f.Name != "Mario Kart" {
		t.Errorf("family: %+v", f)
	}
	if len(f.Consoles) != 2 || f.Consoles[0] != "Switch" || f.Consoles[1] != "3DS" {
		t.Errorf("family consoles are its games' together: %v", f.Consoles)
	}
	if by["7"].Parent != "F" || by["7"].ParentName != "Mario Kart" || by["9"].Parent != "" || by["r"].Family {
		t.Error("parents")
	}
	if !by["r"].General || by["9"].General {
		t.Error("rooms stay rooms, a game stays a game")
	}
}

func TestAGameWhoseFamilyIsMissingStandsAlone(t *testing.T) {
	c := familyServer(t, `{"total":1,"items":[`+game("7", "Mario Kart 7", "3DS", "gone")+`]}`)
	list, _, _ := c.Communities(context.Background(), store.Page{Limit: 600})
	if len(list) != 1 || list[0].Parent != "" {
		t.Errorf("must not hide a game: %+v", list)
	}
}
