package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

const (
	cookieName = "openverse_session"
	flowCookie = "openverse_sso"
	sessionTTL = 7 * 24 * time.Hour
	flowTTL    = 10 * time.Minute
)

// Me is what the site remembers about a signed-in visitor: the OpenPak account, the name
// openpak.org shows for it, the number OpenVerse knows the person by, and whether they accepted
// the community rules. Never a password or a token.
type Me struct {
	Account string `json:"a"`
	Name    string `json:"n"`
	ID      string `json:"i"`
	Rules   bool   `json:"r"`
	Expires int64  `json:"e"`
}

// Initial is the letter on the account chip, like openpak.org's.
func (m Me) Initial() string {
	for _, r := range m.Name {
		return strings.ToUpper(string(r))
	}
	return "?"
}

func (s *Site) sign(data []byte) string {
	m := hmac.New(sha256.New, []byte(s.cfg.SessionKey))
	m.Write(data)
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (s *Site) seal(v any) string {
	b, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(b) + "." + s.sign(b)
}

func (s *Site) open(value string, v any) bool {
	body, sig, ok := strings.Cut(value, ".")
	if !ok {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil || !hmac.Equal([]byte(sig), []byte(s.sign(raw))) {
		return false
	}
	return json.Unmarshal(raw, v) == nil
}

func (s *Site) secure() bool { return strings.HasPrefix(s.cfg.PublicURL, "https://") }

func (s *Site) setCookie(w http.ResponseWriter, name, value, path string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: path, HttpOnly: true, Secure: s.secure(),
		SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}

func (s *Site) setSession(w http.ResponseWriter, m Me) {
	m.Expires = time.Now().Add(sessionTTL).Unix()
	s.setCookie(w, cookieName, s.seal(m), "/", int(sessionTTL.Seconds()))
}

func (s *Site) clearSession(w http.ResponseWriter) { s.setCookie(w, cookieName, "", "/", -1) }

// me returns the signed-in visitor, if the cookie is ours and unexpired.
func (s *Site) me(r *http.Request) (Me, bool) {
	var m Me
	if !s.signInOn() {
		return m, false
	}
	c, err := r.Cookie(cookieName)
	if err != nil || !s.open(c.Value, &m) || m.Account == "" || m.ID == "" || time.Now().Unix() > m.Expires {
		return Me{}, false
	}
	return m, true
}
