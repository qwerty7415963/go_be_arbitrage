package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

//go:embed messages/*.json
var catalogFS embed.FS

type Locale string

const (
	LocaleVI Locale = "vi"
	LocaleEN Locale = "en"
)

func (l Locale) IsValid() bool {
	return l == LocaleVI || l == LocaleEN
}

func Supported() []string {
	return []string{string(LocaleVI), string(LocaleEN)}
}

func isSupported(tag string) bool {
	for _, s := range Supported() {
		if s == tag {
			return true
		}
	}
	return false
}

var (
	catalogs     = map[Locale]map[string]string{}
	catalogsOnce sync.Mutex
	loaded       bool
)

func Load() error {
	catalogsOnce.Lock()
	defer catalogsOnce.Unlock()

	newCatalogs := map[Locale]map[string]string{}
	for _, loc := range []Locale{LocaleVI, LocaleEN} {
		data, err := catalogFS.ReadFile("messages/" + string(loc) + ".json")
		if err != nil {
			return fmt.Errorf("i18n: read catalog %s: %w", loc, err)
		}
		var m map[string]string
		if err := json.Unmarshal(data, &m); err != nil {
			return fmt.Errorf("i18n: parse catalog %s: %w", loc, err)
		}
		newCatalogs[loc] = m
	}

	enKeys := keySet(newCatalogs[LocaleEN])
	viKeys := keySet(newCatalogs[LocaleVI])
	if !equalSets(enKeys, viKeys) {
		missing := diff(viKeys, enKeys)
		extra := diff(enKeys, viKeys)
		return fmt.Errorf("i18n: catalog key mismatch (missing in vi: %v, extra in vi: %v)", missing, extra)
	}
	for loc, m := range newCatalogs {
		for k, v := range m {
			if strings.TrimSpace(v) == "" {
				return fmt.Errorf("i18n: empty value for key %s in %s", k, loc)
			}
		}
	}
	for key := range newCatalogs[LocaleEN] {
		enPH := placeholders(newCatalogs[LocaleEN][key])
		viPH := placeholders(newCatalogs[LocaleVI][key])
		if !equalSets(enPH, viPH) {
			return fmt.Errorf("i18n: placeholder mismatch for key %s (en=%v vi=%v)", key, enPH, viPH)
		}
	}

	catalogs = newCatalogs
	loaded = true
	return nil
}

func ensureLoaded() {
	if !loaded {
		_ = Load()
	}
}

func Translate(locale Locale, key string, params map[string]any, fallback string) string {
	ensureLoaded()
	if !locale.IsValid() {
		locale = LocaleVI
	}
	msg := lookup(locale, key)
	if msg == "" {
		msg = lookup(LocaleEN, key)
	}
	if msg == "" {
		msg = fallback
	}
	if msg == "" {
		msg = key
	}
	for k, v := range params {
		msg = strings.ReplaceAll(msg, "{"+k+"}", fmt.Sprintf("%v", v))
	}
	return msg
}

func lookup(loc Locale, key string) string {
	catalogsOnce.Lock()
	defer catalogsOnce.Unlock()
	if m, ok := catalogs[loc]; ok {
		return m[key]
	}
	return ""
}

func ParseAcceptLanguage(header string) Locale {
	best := ""
	bestQ := -1.0
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tagPart := part
		q := 1.0
		if idx := strings.Index(part, ";"); idx >= 0 {
			tagPart = strings.TrimSpace(part[:idx])
			for _, p := range strings.Split(part[idx+1:], ";") {
				p = strings.TrimSpace(p)
				if strings.HasPrefix(strings.ToLower(p), "q=") {
					v, err := strconv.ParseFloat(strings.TrimSpace(p[2:]), 64)
					if err != nil {
						q = 0
					} else {
						q = v
					}
				}
			}
		}
		if q <= 0 {
			continue
		}
		tag := tagPart
		if i := strings.IndexAny(tag, "-_"); i >= 0 {
			tag = tag[:i]
		}
		tag = strings.ToLower(strings.TrimSpace(tag))
		if !isSupported(tag) {
			continue
		}
		if q > bestQ {
			bestQ = q
			best = tag
		}
	}
	if best == "" {
		return LocaleVI
	}
	return Locale(best)
}

func keySet(m map[string]string) map[string]bool {
	out := map[string]bool{}
	for k := range m {
		out[k] = true
	}
	return out
}

func equalSets(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func diff(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func placeholders(s string) map[string]bool {
	out := map[string]bool{}
	i := 0
	for {
		start := strings.Index(s[i:], "{")
		if start < 0 {
			break
		}
		start += i
		end := strings.Index(s[start:], "}")
		if end < 0 {
			break
		}
		end += start
		name := strings.TrimSpace(s[start+1 : end])
		if name != "" {
			out[name] = true
		}
		i = end + 1
	}
	return out
}
