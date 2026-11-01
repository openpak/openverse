package web

import (
	"fmt"
	"net/url"
	"strings"
)

// Config is read from the environment (see example.env).
type Config struct {
	Addr         string // OPENVERSE_ADDR, default :20195
	APIURL       string // OPENVERSE_API_URL, the miiverse-api HTTP address (private network)
	APIKey       string // PN_MIIVERSE_API_WEB_KEY, the web bridge key
	PublicURL    string // OPENVERSE_PUBLIC_URL, https://verse.openpak.org
	OpenPakURL   string // OPENVERSE_OPENPAK_URL, links to the main site
	CoreURL      string // OPENVERSE_CORE_URL, openpak.org's website, server to server; empty turns sign-in off
	SSOSecret    string // SSO_CLIENT_OPENVERSE_SECRET
	SessionKey   string // OPENVERSE_SESSION_KEY, 32+ characters, signs the cookies
	ContactEmail string // OPENVERSE_CONTACT_EMAIL
	Demo         bool   // OPENVERSE_DEMO=1: fixture data and a sign-in without openpak.org. Never in production.
}

// ConfigFromEnv builds and validates a Config.
func ConfigFromEnv(get func(string) string) (Config, error) {
	c := Config{
		Addr:         or(get("OPENVERSE_ADDR"), ":20195"),
		APIURL:       strings.TrimRight(get("OPENVERSE_API_URL"), "/"),
		APIKey:       get("PN_MIIVERSE_API_WEB_KEY"),
		PublicURL:    strings.TrimRight(or(get("OPENVERSE_PUBLIC_URL"), "https://verse.openpak.org"), "/"),
		OpenPakURL:   strings.TrimRight(or(get("OPENVERSE_OPENPAK_URL"), "https://openpak.org"), "/"),
		CoreURL:      strings.TrimRight(get("OPENVERSE_CORE_URL"), "/"),
		SSOSecret:    get("SSO_CLIENT_OPENVERSE_SECRET"),
		SessionKey:   get("OPENVERSE_SESSION_KEY"),
		ContactEmail: or(get("OPENVERSE_CONTACT_EMAIL"), "contact@openpak.org"),
		Demo:         get("OPENVERSE_DEMO") == "1",
	}
	if !c.Demo && (c.APIURL == "" || c.APIKey == "") {
		return c, fmt.Errorf("set OPENVERSE_API_URL and PN_MIIVERSE_API_WEB_KEY (or OPENVERSE_DEMO=1 for fixture data)")
	}
	for name, v := range map[string]string{"OPENVERSE_API_URL": c.APIURL, "OPENVERSE_PUBLIC_URL": c.PublicURL, "OPENVERSE_CORE_URL": c.CoreURL} {
		if v == "" {
			continue
		}
		if u, err := url.Parse(v); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return c, fmt.Errorf("%s %q is not an http(s) URL", name, v)
		}
	}
	if (c.Demo || (c.CoreURL != "" && c.SSOSecret != "")) && len(c.SessionKey) < 32 {
		return c, fmt.Errorf("OPENVERSE_SESSION_KEY must be at least 32 characters when sign-in is on")
	}
	return c, nil
}

func or(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
