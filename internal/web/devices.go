package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"sync"
	"time"

	"github.com/openpak/openverse/internal/linkcodes"
	"github.com/openpak/openverse/internal/store"
)

// Linked devices: the person types the code a console or an old browser shows, sees what is asking,
// and confirms. The page lists the links the account holds and removes them. It belongs to the
// account menu, not the top bar. Every form carries a token tied to this sign-in, so another site
// cannot approve a code for the person.

type deviceRow struct {
	ID       string
	Class    string
	Badge    string
	Label    string
	Added    string
	LastUsed string
}

type devicesData struct {
	CSRF       string
	Devices    []deviceRow
	Note       string // approved | removed | bad | toomany | slow
	FieldError string // the note, when it belongs under the code field
	Confirm    *struct{ Code, Device string }
}

var badges = map[string]string{"ps3": "PS3", "wii": "Wii", "dsi": "DSi", "x360": "360", "lite": "Web"}

// csrf is derived from the session cookie, so it changes with every sign-in.
func (s *Site) csrf(r *http.Request) string {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	m := hmac.New(sha256.New, []byte(s.cfg.SessionKey))
	m.Write([]byte("devices:" + c.Value))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (s *Site) csrfOK(r *http.Request) bool {
	want := s.csrf(r)
	return want != "" && hmac.Equal([]byte(want), []byte(r.FormValue("k")))
}

func day(t time.Time) string { return t.UTC().Format("2006-01-02") }

func (s *Site) deviceRows(r *http.Request, account string) ([]deviceRow, error) {
	links, err := s.store.DeviceLinks(r.Context(), account)
	if err != nil {
		return nil, err
	}
	tr := s.tr(r)
	rows := make([]deviceRow, 0, len(links))
	for _, l := range links {
		badge := badges[l.Class]
		if badge == "" {
			badge = "Web"
		}
		label := l.Label
		if label == "" {
			label = tr.Str("dev." + l.Class)
		}
		row := deviceRow{ID: l.ID, Class: l.Class, Badge: badge, Label: label, Added: day(l.Created)}
		if l.LastUsed != nil {
			row.LastUsed = day(*l.LastUsed)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (s *Site) devicesPage(w http.ResponseWriter, r *http.Request, me Me, note string, confirm *struct{ Code, Device string }, status int) {
	rows, err := s.deviceRows(r, me.Account)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	d := devicesData{CSRF: s.csrf(r), Devices: rows, Note: note, Confirm: confirm}
	switch note {
	case "bad", "toomany", "slow":
		d.FieldError = note
	}
	noStore(w)
	s.renderKey(w, r, status, "devices", "devices.heading", d)
}

func (s *Site) devices(w http.ResponseWriter, r *http.Request) {
	me, ok := s.need(w, r)
	if !ok {
		return
	}
	note := r.URL.Query().Get("n")
	switch note {
	case "approved", "removed", "bad", "toomany", "slow":
	default:
		note = ""
	}
	s.devicesPage(w, r, me, note, nil, http.StatusOK)
}

// tryLimiter allows a few code attempts a minute per account, so a code cannot be guessed.
type tryLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func (l *tryLimiter) allow(key string, limit int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hits == nil {
		l.hits = map[string][]time.Time{}
	}
	now := time.Now()
	keep := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < window {
			keep = append(keep, t)
		}
	}
	if len(keep) >= limit {
		l.hits[key] = keep
		return false
	}
	l.hits[key] = append(keep, now)
	if len(l.hits) > 5000 {
		l.hits = map[string][]time.Time{key: l.hits[key]}
	}
	return true
}

// deviceWrite is the checks every devices form shares: signed in, the session's own token, and not
// trying too fast. A supervised account may link a device; the Lite service applies the account's
// parental level to every write that device makes.
func (s *Site) deviceWrite(w http.ResponseWriter, r *http.Request, limited bool) (Me, bool) {
	me, ok := s.need(w, r)
	if !ok {
		return me, false
	}
	if limited && !s.tries.allow(me.Account, 10, time.Minute) {
		http.Redirect(w, r, "/devices?n=slow", http.StatusSeeOther)
		return me, false
	}
	if !s.csrfOK(r) {
		http.Redirect(w, r, "/devices?n=bad", http.StatusSeeOther)
		return me, false
	}
	return me, true
}

func (s *Site) devicesCheck(w http.ResponseWriter, r *http.Request) {
	me, ok := s.deviceWrite(w, r, true)
	if !ok {
		return
	}
	code, valid := linkcodes.Normalize(r.FormValue("code"))
	var e *linkcodes.Entry
	if valid && s.codes != nil {
		var err error
		if e, err = linkcodes.Peek(r.Context(), s.codes, code); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if e == nil || e.AccountID != nil {
		http.Redirect(w, r, "/devices?n=bad", http.StatusSeeOther)
		return
	}
	tr := s.tr(r)
	device := tr.Str("dev." + e.DeviceClass)
	if device == "dev."+e.DeviceClass {
		device = tr.Str("dev.lite")
	}
	s.devicesPage(w, r, me, "", &struct{ Code, Device string }{code, device}, http.StatusOK)
}

func (s *Site) devicesApprove(w http.ResponseWriter, r *http.Request) {
	me, ok := s.deviceWrite(w, r, true)
	if !ok {
		return
	}
	code, valid := linkcodes.Normalize(r.FormValue("code"))
	if !valid || s.codes == nil {
		http.Redirect(w, r, "/devices?n=bad", http.StatusSeeOther)
		return
	}
	links, err := s.store.DeviceLinks(r.Context(), me.Account)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if len(links) >= linkcodes.MaxActive {
		http.Redirect(w, r, "/devices?n=toomany", http.StatusSeeOther)
		return
	}
	done, err := linkcodes.Approve(r.Context(), s.codes, code, me.Account, time.Now())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !done {
		http.Redirect(w, r, "/devices?n=bad", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/devices?n=approved", http.StatusSeeOther)
}

func (s *Site) devicesRevoke(w http.ResponseWriter, r *http.Request) {
	me, ok := s.deviceWrite(w, r, false)
	if !ok {
		return
	}
	if err := s.store.RevokeDevice(r.Context(), me.Account, r.FormValue("id")); err != nil {
		if err == store.ErrNotFound {
			http.Redirect(w, r, "/devices?n=bad", http.StatusSeeOther)
			return
		}
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/devices?n=removed", http.StatusSeeOther)
}
