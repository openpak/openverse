package i18n

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var verb = regexp.MustCompile(`%[sd]`)

func TestEveryLanguageHasEveryKeyAndTheSamePlaceholders(t *testing.T) {
	c := Load()
	en := c.Keys(Default)
	for _, lang := range []string{"es", "fr", "de"} {
		got := c.Keys(lang)
		if got == nil {
			t.Fatalf("%s: no catalogue", lang)
		}
		for k, v := range en {
			g, ok := got[k]
			if !ok {
				t.Errorf("%s: missing %q", lang, k)
				continue
			}
			if strings.TrimSpace(g) == "" {
				t.Errorf("%s: %q is empty", lang, k)
			}
			a, b := verb.FindAllString(v, -1), verb.FindAllString(g, -1)
			sort.Strings(a)
			sort.Strings(b)
			if strings.Join(a, "") != strings.Join(b, "") {
				t.Errorf("%s: %q placeholders differ: %q vs %q", lang, k, v, g)
			}
		}
		for k := range got {
			if _, ok := en[k]; !ok {
				t.Errorf("%s: %q is not an English key", lang, k)
			}
		}
	}
}

func TestMatchFallsBackToEnglish(t *testing.T) {
	c := Load()
	for _, tc := range []struct{ choice, accept, want string }{
		{"", "", "en"},
		{"", "es-ES,es;q=0.9,en;q=0.8", "es"},
		{"", "fr-CA", "fr"},
		{"", "de;q=0.5,fr;q=0.9", "fr"},
		{"", "ja,zh;q=0.9", "en"},
		{"", "pt-BR,pt;q=0.9", "en"},
		{"de", "fr", "de"},
		{"xx", "es", "es"},
		{"", "*", "en"},
	} {
		if got := c.Match(tc.choice, tc.accept); got != tc.want {
			t.Errorf("Match(%q, %q) = %q, want %q", tc.choice, tc.accept, got, tc.want)
		}
	}
}

func TestPluralsAndMissingKeys(t *testing.T) {
	c := Load()
	if got := c.For("en").N("n.posts", 1); got != "1 post" {
		t.Errorf("one: %q", got)
	}
	if got := c.For("en").N("n.posts", 2); got != "2 posts" {
		t.Errorf("other: %q", got)
	}
	if got := c.For("fr").N("n.posts", 0); got != "0 message" {
		t.Errorf("fr zero is singular: %q", got)
	}
	if got := c.For("nl").Str("nav.feed"); got != "Feed" {
		t.Errorf("unknown language uses English: %q", got)
	}
}

// Every key a template or Go file asks for must exist in English, so no page can show a raw key.
func TestEveryUsedKeyExists(t *testing.T) {
	en := Load().Keys(Default)
	re := regexp.MustCompile(`\b(?:t|tf|tn|tn1|th|thn)\s+"([a-z0-9_.]+)"|Str\("([a-z0-9_.]+)"|\.F\("([a-z0-9_.]+)"|\.N\("([a-z0-9_.]+)"`)
	var files []string
	for _, root := range []string{"../web", "../web/templates"} {
		m, _ := filepath.Glob(filepath.Join(root, "*.go"))
		h, _ := filepath.Glob(filepath.Join(root, "*.html"))
		files = append(files, m...)
		files = append(files, h...)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, _ := os.ReadFile(f)
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			key := m[1] + m[2] + m[3] + m[4]
			if strings.HasSuffix(key, ".") { // a prefix built at run time (err.<what>.h, dev.<class>)
				continue
			}
			if _, ok := en[key]; ok {
				continue
			}
			if _, ok := en[key+".one"]; ok {
				continue
			}
			t.Errorf("%s: key %q is not in the English catalogue", filepath.Base(f), key)
		}
	}
}
