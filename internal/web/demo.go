package web

import "net/http"

// demoSignIn is the sign-in of OPENVERSE_DEMO=1: no openpak.org, one made-up person, fixture data.
// It exists so the site can be run and photographed without any account. Config refuses to turn it
// on without a session key, and cmd/openverse never wires it to the real store.
func (s *Site) demoSignIn(w http.ResponseWriter, r *http.Request, next string) {
	m := Me{Account: "demo-account", Name: "Tobi", ID: "1800000001", Rules: r.URL.Query().Get("as") != "new"}
	s.setSession(w, m)
	http.Redirect(w, r, next, http.StatusSeeOther)
}
