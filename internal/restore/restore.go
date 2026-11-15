// Package restore matches vault entries to local repositories and writes them back.
package restore

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Emil-02/envboy/internal/backup"
	"github.com/Emil-02/envboy/internal/discover"
	"github.com/Emil-02/envboy/internal/dotenv"
	"github.com/Emil-02/envboy/internal/vault"
)

// Action is what restore would do with one entry.
type Action string

const (
	Create    Action = "create"    // file missing locally
	Identical Action = "identical" // local file matches the vault
	Conflict  Action = "conflict"  // local file differs from the vault
	NoRepo    Action = "no-repo"   // repository not found locally
	Ambiguous Action = "ambiguous" // several local clones could match
	Invalid   Action = "invalid"   // unsafe path in the vault entry
)

// Item is one planned operation.
type Item struct {
	Entry   vault.Entry `json:"-"`
	Repo    string      `json:"repo"`
	File    string      `json:"file"`
	RepoDir string      `json:"repo_dir,omitempty"`
	Target  string      `json:"target,omitempty"`
	Action  Action      `json:"action"`
	Detail  string      `json:"detail,omitempty"`
	// LocalOnly are keys in the local file but not the vault; VaultOnly the reverse.
	LocalOnly []string `json:"local_only_keys,omitempty"`
	VaultOnly []string `json:"vault_only_keys,omitempty"`
}

// Plan is the full set of operations.
type Plan struct {
	Items []Item `json:"items"`
}

// Count returns how many items have action a.
func (p Plan) Count(a Action) int {
	n := 0
	for _, it := range p.Items {
		if it.Action == a {
			n++
		}
	}
	return n
}

// Build plans a restore of v into the local repos found under roots.
// local must come from a discover.Scan with AllRepos set.
func Build(v *vault.Vault, local []discover.Repo, roots []string) Plan {
	byRemote := map[string][]*discover.Repo{}
	byDir := map[string]*discover.Repo{}
	for i := range local {
		r := &local[i]
		byDir[r.Dir] = r
		if r.Remote != "" {
			byRemote[r.Remote] = append(byRemote[r.Remote], r)
		}
	}

	var plan Plan
	for _, e := range v.Entries {
		it := Item{Entry: e, Repo: e.Repo, File: e.File}
		fileOS := filepath.FromSlash(e.File)
		if !filepath.IsLocal(fileOS) {
			it.Action, it.Detail = Invalid, "file path escapes the repository"
			plan.Items = append(plan.Items, it)
			continue
		}
		dir, detail := match(e, byRemote, byDir, roots)
		switch {
		case dir == "" && detail != "":
			it.Action, it.Detail = Ambiguous, detail
		case dir == "":
			it.Action, it.Detail = NoRepo, "clone "+cloneHint(e)+" first"
		default:
			it.RepoDir = dir
			it.Target = filepath.Join(dir, fileOS)
			classify(&it)
		}
		plan.Items = append(plan.Items, it)
	}
	return plan
}

func cloneHint(e vault.Entry) string {
	if e.Remote != "" {
		return e.Remote
	}
	return e.Path
}

func match(e vault.Entry, byRemote map[string][]*discover.Repo, byDir map[string]*discover.Repo, roots []string) (string, string) {
	if e.Remote != "" {
		cands := byRemote[e.Remote]
		switch len(cands) {
		case 1:
			return cands[0].Dir, ""
		case 0:
		default:
			for _, c := range cands {
				if c.RelPath == e.Path {
					return c.Dir, ""
				}
			}
			var sameBase []*discover.Repo
			for _, c := range cands {
				if filepath.Base(c.Dir) == filepath.Base(filepath.FromSlash(e.Path)) {
					sameBase = append(sameBase, c)
				}
			}
			if len(sameBase) == 1 {
				return sameBase[0].Dir, ""
			}
			return "", fmt.Sprintf("%d local clones of %s", len(cands), e.Remote)
		}
	}
	pathOS := filepath.FromSlash(e.Path)
	if e.Path != "." && !filepath.IsLocal(pathOS) {
		return "", ""
	}
	for _, root := range roots {
		abs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		dir := filepath.Join(abs, pathOS)
		st, err := os.Stat(dir)
		if err != nil || !st.IsDir() {
			continue
		}
		// Never drop secrets into a different repository that happens to sit at the old path.
		if r, ok := byDir[dir]; ok && r.Remote != "" && e.Remote != "" && r.Remote != e.Remote {
			continue
		}
		return dir, ""
	}
	return "", ""
}

func classify(it *Item) {
	data, err := os.ReadFile(it.Target)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		it.Action = Create
	case err != nil:
		it.Action, it.Detail = Conflict, err.Error()
	case bytes.Equal(data, it.Entry.Content) || backup.Hash(data) == it.Entry.SHA256:
		it.Action = Identical
	default:
		it.Action = Conflict
		it.LocalOnly, it.VaultOnly = dotenv.Diff(it.Entry.Keys, dotenv.Names(data))
	}
}

// Options controls Apply.
type Options struct {
	// Force overwrites conflicting files.
	Force bool
	// BackupExisting renames conflicting files to <name>.bak before writing.
	BackupExisting bool
	// Filter limits which items are applied (nil applies all).
	Filter func(Item) bool
}

// Result summarises Apply.
type Result struct {
	Written  []Item
	BackedUp []string
	Skipped  []Item
	Errors   []error
}

// Apply executes the plan.
func Apply(p Plan, opt Options) Result {
	var res Result
	for _, it := range p.Items {
		if opt.Filter != nil && !opt.Filter(it) {
			continue
		}
		switch it.Action {
		case Create:
		case Conflict:
			if !opt.Force && !opt.BackupExisting {
				res.Skipped = append(res.Skipped, it)
				continue
			}
			if opt.BackupExisting {
				bak, err := moveAside(it.Target)
				if err != nil {
					res.Errors = append(res.Errors, err)
					continue
				}
				res.BackedUp = append(res.BackedUp, bak)
			}
		case Identical:
			continue
		default:
			res.Skipped = append(res.Skipped, it)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(it.Target), 0o755); err != nil {
			res.Errors = append(res.Errors, err)
			continue
		}
		if err := vault.WriteFileAtomic(it.Target, it.Entry.Content, 0o600); err != nil {
			res.Errors = append(res.Errors, err)
			continue
		}
		res.Written = append(res.Written, it)
	}
	return res
}

func moveAside(path string) (string, error) {
	bak := path + ".bak"
	if _, err := os.Lstat(bak); err == nil {
		bak = path + ".bak." + time.Now().Format("20060102-150405")
	}
	return bak, os.Rename(path, bak)
}

// Status compares the vault with the disk.
type Status struct {
	Plan Plan `json:"plan"`
	// NotBackedUp are local env files the vault does not contain.
	NotBackedUp []LocalFile `json:"not_backed_up"`
}

// LocalFile is an env file found on disk.
type LocalFile struct {
	Repo string `json:"repo"`
	Dir  string `json:"repo_dir"`
	File string `json:"file"`
}

// BuildStatus plans a restore and also lists local files missing from the vault.
func BuildStatus(v *vault.Vault, local []discover.Repo, roots []string) Status {
	plan := Build(v, local, roots)
	covered := map[string]bool{}
	for _, it := range plan.Items {
		if it.Target != "" {
			covered[it.Target] = true
		}
	}
	var missing []LocalFile
	for _, r := range local {
		for _, f := range r.EnvFiles {
			if !covered[f.Path] {
				missing = append(missing, LocalFile{Repo: r.Name(), Dir: r.Dir, File: f.Rel})
			}
		}
	}
	sort.Slice(missing, func(i, j int) bool {
		if missing[i].Repo != missing[j].Repo {
			return missing[i].Repo < missing[j].Repo
		}
		return missing[i].File < missing[j].File
	})
	return Status{Plan: plan, NotBackedUp: missing}
}
