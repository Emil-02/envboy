// Package discover walks directory trees and finds repositories, env files
// and their committed templates.
package discover

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Emil-02/envboy/internal/config"
	"github.com/Emil-02/envboy/internal/gitinfo"
)

// MaxFileSize is the largest file treated as an env file.
const MaxFileSize = 1 << 20

// Options controls a scan.
type Options struct {
	Patterns        []string
	Exclude         []string
	TemplateMarkers []string
	SkipDirs        []string
	// AllRepos also returns git repositories that have no env files or templates.
	AllRepos bool
}

// FromConfig builds Options from a config.
func FromConfig(c config.Config) Options {
	return Options{Patterns: c.Patterns, Exclude: c.Exclude, TemplateMarkers: c.TemplateMarkers, SkipDirs: c.SkipDirs}
}

// File is an env file or template inside a repo.
type File struct {
	Path    string      `json:"-"`
	Rel     string      `json:"file"` // slash-separated, relative to Repo.Dir
	Size    int64       `json:"size"`
	Mode    fs.FileMode `json:"-"`
	Tracked bool        `json:"tracked"`
	Ignored bool        `json:"ignored"`
}

// Risk describes how exposed an env file is.
type Risk string

const (
	RiskNone      Risk = ""          // ignored by git, or not a git repo
	RiskUnignored Risk = "unignored" // not matched by .gitignore: one `git add .` away from a leak
	RiskTracked   Risk = "tracked"   // already committed
)

// Repo is a git work tree, or for non-git folders the directory holding the env files.
type Repo struct {
	Dir     string `json:"dir"`
	Root    string `json:"root"`
	RelPath string `json:"path"` // slash-separated, relative to Root
	IsGit   bool   `json:"git"`
	Remote  string `json:"remote,omitempty"` // normalized, never contains credentials
	// Key identifies the repo in a vault: the remote, or "path:<RelPath>" without one.
	Key       string `json:"key"`
	EnvFiles  []File `json:"env_files"`
	Templates []File `json:"templates"`
	// Skipped lists env-like files that were not read (too large or unreadable).
	Skipped []string `json:"skipped,omitempty"`
}

// Risk returns the risk of f in r.
func (r *Repo) Risk(f File) Risk {
	if !r.IsGit {
		return RiskNone
	}
	if f.Tracked {
		return RiskTracked
	}
	if !f.Ignored {
		return RiskUnignored
	}
	return RiskNone
}

// Name is a short display name.
func (r *Repo) Name() string {
	if r.RelPath == "." || r.RelPath == "" {
		return filepath.Base(r.Dir)
	}
	return r.RelPath
}

// Kind classifies a file name.
type Kind int

const (
	KindOther Kind = iota
	KindEnv
	KindTemplate
)

// Classify reports whether name is an env file, a template, or neither.
func (o Options) Classify(name string) Kind {
	matched := false
	for _, p := range o.Patterns {
		if ok, _ := filepath.Match(p, name); ok {
			matched = true
			break
		}
	}
	if !matched {
		return KindOther
	}
	for _, p := range o.Exclude {
		if ok, _ := filepath.Match(p, name); ok {
			return KindOther
		}
	}
	for seg := range strings.SplitSeq(strings.ToLower(name), ".") {
		for _, m := range o.TemplateMarkers {
			if seg == m {
				return KindTemplate
			}
		}
	}
	return KindEnv
}

// Scan walks roots and returns the repos found, sorted by root then path.
func Scan(roots []string, opt Options) ([]Repo, error) {
	skip := map[string]bool{}
	for _, d := range opt.SkipDirs {
		skip[d] = true
	}
	var all []Repo
	seenDirs := map[string]bool{}
	for _, root := range roots {
		abs, err := filepath.Abs(config.Expand(root))
		if err != nil {
			return nil, err
		}
		st, err := os.Stat(abs)
		if err != nil {
			return nil, err
		}
		if !st.IsDir() {
			return nil, &fs.PathError{Op: "scan", Path: abs, Err: errors.New("not a directory")}
		}
		repos, err := scanRoot(abs, opt, skip)
		if err != nil {
			return nil, err
		}
		for _, r := range repos {
			if !seenDirs[r.Dir] {
				seenDirs[r.Dir] = true
				all = append(all, r)
			}
		}
	}
	return all, nil
}

func scanRoot(root string, opt Options, skip map[string]bool) ([]Repo, error) {
	gitDirs := map[string]bool{}
	// The root itself may sit inside a work tree.
	if top := enclosingRepo(root); top != "" {
		gitDirs[top] = true
	}
	byDir := map[string]*Repo{}
	get := func(dir string, isGit bool) *Repo {
		if r, ok := byDir[dir]; ok {
			return r
		}
		r := &Repo{Dir: dir, Root: root, IsGit: isGit}
		byDir[dir] = r
		return r
	}
	if opt.AllRepos {
		for d := range gitDirs {
			get(d, true)
		}
	}

	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() && p != root {
				return fs.SkipDir // unreadable directory
			}
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && skip[name] {
				return fs.SkipDir
			}
			if _, err := os.Lstat(filepath.Join(p, ".git")); err == nil {
				gitDirs[p] = true
				if opt.AllRepos {
					get(p, true)
				}
			}
			return nil
		}
		kind := opt.Classify(name)
		if kind == KindOther || !d.Type().IsRegular() {
			return nil
		}
		dir := filepath.Dir(p)
		owner, isGit := dir, false
		if top := nearest(dir, gitDirs); top != "" {
			owner, isGit = top, true
		}
		r := get(owner, isGit)
		rel, _ := filepath.Rel(owner, p)
		info, err := d.Info()
		if err != nil || info.Size() > MaxFileSize {
			r.Skipped = append(r.Skipped, filepath.ToSlash(rel))
			return nil
		}
		f := File{Path: p, Rel: filepath.ToSlash(rel), Size: info.Size(), Mode: info.Mode().Perm()}
		if kind == KindTemplate {
			r.Templates = append(r.Templates, f)
		} else {
			r.EnvFiles = append(r.EnvFiles, f)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	repos := make([]Repo, 0, len(byDir))
	for _, r := range byDir {
		r.RelPath = relPath(root, r.Dir)
		if r.IsGit {
			r.Remote = gitinfo.NormalizeRemote(gitinfo.Remote(r.Dir))
			annotate(r)
		}
		r.Key = "path:" + r.RelPath
		if r.Remote != "" {
			r.Key = r.Remote
		}
		repos = append(repos, *r)
	}
	sort.Slice(repos, func(i, j int) bool { return repos[i].RelPath < repos[j].RelPath })
	disambiguate(repos)
	return repos, nil
}

// disambiguate makes keys unique when several clones share one remote
// (e.g. git worktrees), by appending the relative path.
func disambiguate(repos []Repo) {
	count := map[string]int{}
	for _, r := range repos {
		count[r.Key]++
	}
	for i := range repos {
		if count[repos[i].Key] > 1 {
			repos[i].Key = repos[i].Key + "@" + repos[i].RelPath
		}
	}
}

func annotate(r *Repo) {
	var rels []string
	for _, f := range r.EnvFiles {
		rels = append(rels, f.Rel)
	}
	for _, f := range r.Templates {
		rels = append(rels, f.Rel)
	}
	if len(rels) == 0 {
		return
	}
	tracked, _ := gitinfo.Tracked(r.Dir, rels)
	ignored, _ := gitinfo.Ignored(r.Dir, rels)
	for i := range r.EnvFiles {
		r.EnvFiles[i].Tracked = tracked[r.EnvFiles[i].Rel]
		r.EnvFiles[i].Ignored = ignored[r.EnvFiles[i].Rel]
	}
	for i := range r.Templates {
		r.Templates[i].Tracked = tracked[r.Templates[i].Rel]
		r.Templates[i].Ignored = ignored[r.Templates[i].Rel]
	}
}

// nearest returns the closest ancestor of dir (inclusive) that is a known git
// work tree. gitDirs only holds dirs under the root plus the root's enclosing repo.
func nearest(dir string, gitDirs map[string]bool) string {
	for d := dir; ; {
		if gitDirs[d] {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

func enclosingRepo(dir string) string {
	for d := dir; ; {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

func relPath(root, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.Base(dir)
	}
	return filepath.ToSlash(rel)
}
