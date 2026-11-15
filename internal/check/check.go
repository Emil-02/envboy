// Package check compares env files against their committed templates.
package check

import (
	"os"
	"path"
	"sort"

	"github.com/Emil-02/envboy/internal/discover"
	"github.com/Emil-02/envboy/internal/dotenv"
)

// Finding compares one env file with its template, or reports a template
// whose env file is missing.
type Finding struct {
	Repo     string `json:"repo"`
	RepoDir  string `json:"repo_dir"`
	File     string `json:"file,omitempty"` // empty when the env file does not exist
	Template string `json:"template"`
	// Missing are keys in the template but not in the env file.
	Missing []string `json:"missing,omitempty"`
	// Extra are keys in the env file but not in the template.
	Extra []string `json:"extra,omitempty"`
	// Empty are keys present in the env file with an empty value.
	Empty []string `json:"empty,omitempty"`
	// NoEnvFile is set when the template exists but no env file does.
	NoEnvFile bool   `json:"no_env_file,omitempty"`
	Error     string `json:"error,omitempty"`
}

// OK reports whether nothing at all was found.
func (f Finding) OK() bool {
	return !f.NoEnvFile && len(f.Missing) == 0 && len(f.Extra) == 0 && len(f.Empty) == 0 && f.Error == ""
}

// Failed reports whether f should fail a check. Strict also fails on extra
// keys, empty values and missing env files.
func (f Finding) Failed(strict bool) bool {
	if f.Error != "" || len(f.Missing) > 0 {
		return true
	}
	return strict && !f.OK()
}

// fallbackTemplates are generic templates for any env file in the same directory.
var fallbackTemplates = []string{".env.example", ".env.sample", ".env.template", ".env.dist"}

// Repo checks every env file in r.
func Repo(r discover.Repo) []Finding {
	templates := map[string]discover.File{}
	for _, t := range r.Templates {
		templates[t.Rel] = t
	}
	used := map[string]bool{}
	var out []Finding
	for _, f := range r.EnvFiles {
		t, ok := templateFor(f.Rel, templates)
		if !ok {
			continue
		}
		used[t.Rel] = true
		out = append(out, compare(r, f, t))
	}
	for _, t := range r.Templates {
		if used[t.Rel] {
			continue
		}
		out = append(out, Finding{Repo: r.Name(), RepoDir: r.Dir, Template: t.Rel, NoEnvFile: true})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].File+out[i].Template < out[j].File+out[j].Template
	})
	return out
}

// All checks every repo.
func All(repos []discover.Repo) []Finding {
	var out []Finding
	for _, r := range repos {
		out = append(out, Repo(r)...)
	}
	return out
}

func templateFor(rel string, templates map[string]discover.File) (discover.File, bool) {
	dir, name := path.Split(rel)
	for _, suffix := range []string{".example", ".sample", ".template", ".dist"} {
		if t, ok := templates[dir+name+suffix]; ok {
			return t, true
		}
	}
	for _, n := range fallbackTemplates {
		if t, ok := templates[dir+n]; ok {
			return t, true
		}
	}
	return discover.File{}, false
}

func compare(r discover.Repo, f, t discover.File) Finding {
	res := Finding{Repo: r.Name(), RepoDir: r.Dir, File: f.Rel, Template: t.Rel}
	envData, err := os.ReadFile(f.Path)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	tplData, err := os.ReadFile(t.Path)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	envKeys := dotenv.Parse(envData)
	res.Extra, res.Missing = dotenv.Diff(dotenv.Names(tplData), dotenv.Names(envData))
	// Later assignments override earlier ones, as in every dotenv loader.
	empty := map[string]bool{}
	for _, k := range envKeys {
		empty[k.Name] = k.Empty
	}
	for name, isEmpty := range empty {
		if isEmpty {
			res.Empty = append(res.Empty, name)
		}
	}
	sort.Strings(res.Empty)
	return res
}
