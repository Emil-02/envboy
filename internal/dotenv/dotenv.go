// Package dotenv extracts key names from .env files. It deliberately never
// returns values: the only value-derived fact it exposes is whether a value is empty.
package dotenv

import (
	"regexp"
	"sort"
	"strings"
)

// Key is one assignment found in a file.
type Key struct {
	Name  string
	Empty bool
	Line  int
}

var keyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.\-]*$`)

// Parse returns the assignments in data in file order. Lines that are not
// assignments are ignored. Quoted values may span multiple lines.
func Parse(data []byte) []Key {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.TrimPrefix(text, "\ufeff")
	lines := strings.Split(text, "\n")

	var keys []Key
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "export "); ok {
			line = strings.TrimSpace(rest)
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		name := strings.TrimSpace(line[:eq])
		if !keyRe.MatchString(name) {
			continue
		}
		startLine := i + 1
		val := strings.TrimSpace(line[eq+1:])

		empty := false
		switch {
		case val == "" || val[0] == '#':
			empty = true
		case val[0] == '"' || val[0] == '\'' || val[0] == '`':
			body, consumed := readQuoted(val[1:], val[0], lines[i+1:])
			i += consumed
			empty = body == ""
		default:
			if idx := strings.Index(val, " #"); idx >= 0 {
				val = strings.TrimSpace(val[:idx])
			}
			empty = val == ""
		}
		keys = append(keys, Key{Name: name, Empty: empty, Line: startLine})
	}
	return keys
}

// readQuoted returns the quoted body (without quotes) and how many extra
// lines were consumed. An unterminated quote consumes the rest of the file.
func readQuoted(first string, q byte, rest []string) (string, int) {
	var b strings.Builder
	cur := first
	consumed := 0
	for {
		for j := 0; j < len(cur); j++ {
			c := cur[j]
			if c == '\\' && q == '"' && j+1 < len(cur) {
				b.WriteByte(cur[j+1])
				j++
				continue
			}
			if c == q {
				return b.String(), consumed
			}
			b.WriteByte(c)
		}
		if consumed >= len(rest) {
			return b.String(), consumed
		}
		b.WriteByte('\n')
		cur = rest[consumed]
		consumed++
	}
}

// Names returns the sorted, de-duplicated key names in data.
func Names(data []byte) []string {
	seen := map[string]bool{}
	var res []string
	for _, k := range Parse(data) {
		if !seen[k.Name] {
			seen[k.Name] = true
			res = append(res, k.Name)
		}
	}
	sort.Strings(res)
	return res
}

// Diff compares two key lists and returns the keys only in `to` (added) and only in `from` (removed).
func Diff(from, to []string) (added, removed []string) {
	set := func(list []string) map[string]bool {
		m := make(map[string]bool, len(list))
		for _, k := range list {
			m[k] = true
		}
		return m
	}
	f, t := set(from), set(to)
	for k := range t {
		if !f[k] {
			added = append(added, k)
		}
	}
	for k := range f {
		if !t[k] {
			removed = append(removed, k)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}
