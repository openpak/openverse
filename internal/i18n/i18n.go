// Package i18n holds OpenVerse's web strings. English is the source: es, fr and de are translated,
// every other language the browser asks for falls back to English. Catalogues are flat JSON files
// (locales/<lang>.json); a plural is two keys, "<key>.one" and "<key>.other".
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"sort"
	"strconv"
	"strings"
)

//go:embed locales/*.json
var files embed.FS

// Default is the fallback language and the source of every key.
const Default = "en"

// Catalog is every language's strings.
type Catalog struct {
	langs map[string]map[string]string
}

// Load reads the embedded catalogues. It panics on a malformed file: that is a build error.
func Load() *Catalog {
	c := &Catalog{langs: map[string]map[string]string{}}
	entries, err := files.ReadDir("locales")
	if err != nil {
		panic(err)
	}
	for _, e := range entries {
		raw, err := files.ReadFile("locales/" + e.Name())
		if err != nil {
			panic(err)
		}
		m := map[string]string{}
		if err := json.Unmarshal(raw, &m); err != nil {
			panic(fmt.Sprintf("i18n: %s: %v", e.Name(), err))
		}
		c.langs[strings.TrimSuffix(e.Name(), ".json")] = m
	}
	if c.langs[Default] == nil {
		panic("i18n: no English catalogue")
	}
	return c
}

// Languages lists the translated languages, English first.
func (c *Catalog) Languages() []string {
	out := []string{Default}
	var rest []string
	for l := range c.langs {
		if l != Default {
			rest = append(rest, l)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// Keys is the key set of one language.
func (c *Catalog) Keys(lang string) map[string]string { return c.langs[lang] }

// Match picks a language from an explicit choice (cookie or ?lang=) and then the Accept-Language
// header. Anything that is not translated is English.
func (c *Catalog) Match(choice, accept string) string {
	if l := c.base(choice); l != "" {
		return l
	}
	type cand struct {
		tag string
		q   float64
	}
	var list []cand
	for _, part := range strings.Split(accept, ",") {
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		q := 1.0
		if _, v, ok := strings.Cut(strings.TrimSpace(params), "q="); ok {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				q = f
			}
		}
		if tag != "" && q > 0 {
			list = append(list, cand{tag, q})
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].q > list[j].q })
	for _, cd := range list {
		if l := c.base(cd.tag); l != "" {
			return l
		}
	}
	return Default
}

// Pick is the translated language for an explicit tag (fr-CA -> fr), or "" if there is none.
func (c *Catalog) Pick(tag string) string { return c.base(tag) }

// base maps "fr-CA" to "fr" when fr is translated, "" when it is not.
func (c *Catalog) base(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if i := strings.IndexAny(tag, "-_"); i > 0 {
		tag = tag[:i]
	}
	if _, ok := c.langs[tag]; ok {
		return tag
	}
	return ""
}

// T translates for one language.
type T struct {
	c    *Catalog
	Lang string
}

// For returns the translator for a language.
func (c *Catalog) For(lang string) T {
	if _, ok := c.langs[lang]; !ok {
		lang = Default
	}
	return T{c: c, Lang: lang}
}

// Str is the string for key, in the language or else English. An unknown key shows as itself, which
// the tests catch.
func (t T) Str(key string) string {
	if s, ok := t.c.langs[t.Lang][key]; ok {
		return s
	}
	if s, ok := t.c.langs[Default][key]; ok {
		return s
	}
	return key
}

// F formats the string for key with fmt verbs.
func (t T) F(key string, args ...any) string { return fmt.Sprintf(t.Str(key), args...) }

// N picks key+".one" for 1 (and 0 in French) and key+".other" otherwise, and formats it with n.
func (t T) N(key string, n int) string {
	return fmt.Sprintf(t.Str(key+t.form(n)), n)
}

// W is N without the number, for "<b>3</b> posts".
func (t T) W(key string, n int) string {
	s := t.N(key, n)
	return strings.TrimPrefix(s, strconv.Itoa(n)+" ")
}

func (t T) form(n int) string {
	if n == 1 || (t.Lang == "fr" && n == 0) {
		return ".one"
	}
	return ".other"
}

// H formats a catalogue string that holds trusted markup (links). Plain arguments are HTML-escaped;
// template.HTML arguments pass through.
func (t T) H(key string, args ...any) template.HTML { return t.hfmt(t.Str(key), args) }

// HN is H with a plural form chosen by n.
func (t T) HN(key string, n int, args ...any) template.HTML {
	return t.hfmt(t.Str(key+t.form(n)), args)
}

func (t T) hfmt(format string, args []any) template.HTML {
	safe := make([]any, len(args))
	for i, a := range args {
		switch v := a.(type) {
		case template.HTML:
			safe[i] = string(v)
		case string:
			safe[i] = html.EscapeString(v)
		default:
			safe[i] = v
		}
	}
	return template.HTML(fmt.Sprintf(format, safe...))
}
