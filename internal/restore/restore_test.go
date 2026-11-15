package restore_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Emil-02/envboy/internal/backup"
	"github.com/Emil-02/envboy/internal/config"
	"github.com/Emil-02/envboy/internal/discover"
	"github.com/Emil-02/envboy/internal/restore"
	"github.com/Emil-02/envboy/internal/testutil"
	"github.com/Emil-02/envboy/internal/vault"
)

func entry(repo, remote, path, file, content string) vault.Entry {
	return vault.Entry{Repo: repo, Remote: remote, Path: path, File: file,
		Content: []byte(content), SHA256: backup.Hash([]byte(content)), Keys: []string{"A"}}
}

func scanAll(t *testing.T, root string) []discover.Repo {
	t.Helper()
	opt := discover.FromConfig(config.Default())
	opt.AllRepos = true
	repos, err := discover.Scan([]string{root}, opt)
	if err != nil {
		t.Fatal(err)
	}
	return repos
}

func byFile(p restore.Plan) map[string]restore.Item {
	m := map[string]restore.Item{}
	for _, it := range p.Items {
		m[it.Repo+"|"+it.File] = it
	}
	return m
}

func TestPlanAndApply(t *testing.T) {
	root := t.TempDir()
	// Same remote, different folder name and depth than on the old machine.
	web := testutil.Repo(t, filepath.Join(root, "deep", "renamed-web"), "https://github.com/me/web.git")
	api := testutil.Repo(t, filepath.Join(root, "api"), "git@github.com:me/api.git")
	testutil.Write(t, filepath.Join(api, ".env"), "A=local\nONLY_LOCAL=1\n")
	same := testutil.Repo(t, filepath.Join(root, "same"), "git@github.com:me/same.git")
	testutil.Write(t, filepath.Join(same, ".env"), "A=1\n")
	// A different repository now lives at the old path of "moved".
	testutil.Repo(t, filepath.Join(root, "moved"), "git@github.com:me/other.git")
	testutil.Write(t, filepath.Join(root, "plain", "keep.txt"), "")

	v := vault.New("t")
	v.Entries = []vault.Entry{
		entry("github.com/me/web", "github.com/me/web", "web", "apps/api/.env", "A=1\n"),
		entry("github.com/me/api", "github.com/me/api", "api", ".env", "A=vault\n"),
		entry("github.com/me/same", "github.com/me/same", "same", ".env", "A=1\n"),
		entry("github.com/me/moved", "github.com/me/moved", "moved", ".env", "A=1\n"),
		entry("path:plain", "", "plain", ".env", "P=1\n"),
		entry("path:gone", "", "gone", ".env", "G=1\n"),
		entry("evil", "github.com/me/web", "web", "../../escape.env", "X=1\n"),
	}
	plan := restore.Build(v, scanAll(t, root), []string{root})
	items := byFile(plan)

	want := map[string]restore.Action{
		"github.com/me/web|apps/api/.env": restore.Create,
		"github.com/me/api|.env":          restore.Conflict,
		"github.com/me/same|.env":         restore.Identical,
		"github.com/me/moved|.env":        restore.NoRepo,
		"path:plain|.env":                 restore.Create,
		"path:gone|.env":                  restore.NoRepo,
		"evil|../../escape.env":           restore.Invalid,
	}
	for k, a := range want {
		if items[k].Action != a {
			t.Errorf("%s: action %q, want %q (%s)", k, items[k].Action, a, items[k].Detail)
		}
	}
	if it := items["github.com/me/web|apps/api/.env"]; it.RepoDir != web {
		t.Errorf("web matched %q, want %q", it.RepoDir, web)
	}
	if it := items["github.com/me/api|.env"]; len(it.LocalOnly) != 1 || it.LocalOnly[0] != "ONLY_LOCAL" {
		t.Errorf("conflict key diff = %+v", it)
	}

	res := restore.Apply(plan, restore.Options{})
	if len(res.Written) != 2 || len(res.Errors) != 0 {
		t.Fatalf("apply: %+v", res)
	}
	target := filepath.Join(web, "apps", "api", ".env")
	if testutil.Read(t, target) != "A=1\n" {
		t.Fatal("web .env not restored")
	}
	if st, _ := os.Stat(target); runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatalf("restored mode = %v", st.Mode().Perm())
	}
	if testutil.Read(t, filepath.Join(api, ".env")) != "A=local\nONLY_LOCAL=1\n" {
		t.Fatal("conflicting file must be kept by default")
	}
	if _, err := os.Stat(filepath.Join(root, "escape.env")); err == nil {
		t.Fatal("path traversal escaped the repo")
	}

	res = restore.Apply(plan, restore.Options{BackupExisting: true, Filter: func(it restore.Item) bool { return it.Repo == "github.com/me/api" }})
	if len(res.Written) != 1 || len(res.BackedUp) != 1 {
		t.Fatalf("backup-existing: %+v", res)
	}
	if testutil.Read(t, filepath.Join(api, ".env")) != "A=vault\n" || testutil.Read(t, res.BackedUp[0]) != "A=local\nONLY_LOCAL=1\n" {
		t.Fatal("backup-existing did not swap files")
	}
}

func TestAmbiguousClones(t *testing.T) {
	root := t.TempDir()
	testutil.Repo(t, filepath.Join(root, "one"), "git@github.com:me/x.git")
	testutil.Repo(t, filepath.Join(root, "two"), "git@github.com:me/x.git")
	exact := testutil.Repo(t, filepath.Join(root, "sub", "x"), "git@github.com:me/x.git")
	v := vault.New("t")
	v.Entries = []vault.Entry{
		entry("github.com/me/x@old", "github.com/me/x", "elsewhere/nope", ".env", "A=1"),
		entry("github.com/me/x@sub/x", "github.com/me/x", "sub/x", ".env", "A=1"),
		entry("github.com/me/x@x", "github.com/me/x", "projects/x", ".env.local", "A=1"),
	}
	items := byFile(restore.Build(v, scanAll(t, root), []string{root}))
	if items["github.com/me/x@old|.env"].Action != restore.Ambiguous {
		t.Errorf("want ambiguous, got %+v", items["github.com/me/x@old|.env"])
	}
	if it := items["github.com/me/x@sub/x|.env"]; it.RepoDir != exact {
		t.Errorf("same relative path should win, got %+v", it)
	}
	if it := items["github.com/me/x@x|.env.local"]; it.RepoDir != exact {
		t.Errorf("same folder name should win, got %+v", it)
	}
}

func TestStatus(t *testing.T) {
	root := t.TempDir()
	r := testutil.Repo(t, filepath.Join(root, "r"), "https://github.com/me/r")
	testutil.Write(t, filepath.Join(r, ".env"), "A=1\n")
	testutil.Write(t, filepath.Join(r, ".env.new"), "N=1\n")
	v := vault.New("t")
	v.Entries = []vault.Entry{entry("github.com/me/r", "github.com/me/r", "r", ".env", "A=1\n")}
	st := restore.BuildStatus(v, scanAll(t, root), []string{root})
	if st.Plan.Count(restore.Identical) != 1 || len(st.NotBackedUp) != 1 || st.NotBackedUp[0].File != ".env.new" {
		t.Fatalf("status = %+v", st)
	}
}
