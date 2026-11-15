package discover_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Emil-02/envboy/internal/config"
	"github.com/Emil-02/envboy/internal/discover"
	"github.com/Emil-02/envboy/internal/testutil"
)

func opts() discover.Options { return discover.FromConfig(config.Default()) }

func TestClassify(t *testing.T) {
	o := opts()
	cases := map[string]discover.Kind{
		".env":                discover.KindEnv,
		".env.local":          discover.KindEnv,
		".env.production":     discover.KindEnv,
		"docker.env":          discover.KindEnv,
		".env.example":        discover.KindTemplate,
		".env.local.example":  discover.KindTemplate,
		".env.sample":         discover.KindTemplate,
		"app.env.template":    discover.KindOther,
		"vite-env.d.ts":       discover.KindOther,
		"config.env.ts":       discover.KindOther,
		".env.bak":            discover.KindOther,
		".env.local.bak.2026": discover.KindOther,
		".env~":               discover.KindOther,
		".envrc":              discover.KindOther,
		"environment.ts":      discover.KindOther,
		"README.md":           discover.KindOther,
	}
	for name, want := range cases {
		if got := o.Classify(name); got != want {
			t.Errorf("Classify(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestScan(t *testing.T) {
	root := t.TempDir()
	web := testutil.Repo(t, filepath.Join(root, "web"), "git@github.com:Me/Web.git")
	testutil.Write(t, filepath.Join(web, ".gitignore"), ".env\n")
	testutil.Write(t, filepath.Join(web, ".env"), "A=1\n")
	testutil.Write(t, filepath.Join(web, ".env.example"), "A=\n")
	testutil.Write(t, filepath.Join(web, "apps/api/.env.local"), "B=1\n")
	testutil.Write(t, filepath.Join(web, "node_modules/pkg/.env"), "SKIP=1\n")

	sub := testutil.Repo(t, filepath.Join(web, "vendor-not-skipped", "nested"), "")
	testutil.Write(t, filepath.Join(sub, ".env"), "N=1\n")

	testutil.Write(t, filepath.Join(root, "plain/.env"), "P=1\n")
	testutil.Write(t, filepath.Join(root, "plain/.env.bak"), "P=0\n")
	big := make([]byte, discover.MaxFileSize+1)
	if err := os.WriteFile(filepath.Join(root, "plain/huge.env"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.Repo(t, filepath.Join(root, "empty"), "https://github.com/me/empty")

	repos, err := discover.Scan([]string{root}, opts())
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]discover.Repo{}
	for _, r := range repos {
		byPath[r.RelPath] = r
	}
	if len(repos) != 3 {
		t.Fatalf("want 3 repos, got %d: %+v", len(repos), byPath)
	}

	w := byPath["web"]
	if !w.IsGit || w.Remote != "github.com/me/web" || w.Key != "github.com/me/web" {
		t.Fatalf("web repo = %+v", w)
	}
	if len(w.EnvFiles) != 2 || len(w.Templates) != 1 {
		t.Fatalf("web files = %+v templates = %+v", w.EnvFiles, w.Templates)
	}
	for _, f := range w.EnvFiles {
		switch f.Rel {
		case ".env":
			if w.Risk(f) != discover.RiskNone {
				t.Errorf(".env risk = %q", w.Risk(f))
			}
		case "apps/api/.env.local":
			if w.Risk(f) != discover.RiskUnignored {
				t.Errorf(".env.local risk = %q", w.Risk(f))
			}
		default:
			t.Errorf("unexpected file %s", f.Rel)
		}
	}

	n := byPath["web/vendor-not-skipped/nested"]
	if !n.IsGit || n.Key != "path:web/vendor-not-skipped/nested" || len(n.EnvFiles) != 1 {
		t.Fatalf("nested repo = %+v", n)
	}

	p := byPath["plain"]
	if p.IsGit || p.Key != "path:plain" || len(p.EnvFiles) != 1 || len(p.Skipped) != 1 {
		t.Fatalf("plain = %+v", p)
	}

	opt := opts()
	opt.AllRepos = true
	all, err := discover.Scan([]string{root}, opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Fatalf("AllRepos: want 4 repos, got %d", len(all))
	}
}

func TestScanInsideRepo(t *testing.T) {
	root := t.TempDir()
	repo := testutil.Repo(t, filepath.Join(root, "repo"), "https://github.com/me/repo")
	testutil.Write(t, filepath.Join(repo, "svc/.env"), "A=1\n")
	repos, err := discover.Scan([]string{filepath.Join(repo, "svc")}, opts())
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].Dir != repo || repos[0].EnvFiles[0].Rel != "svc/.env" {
		t.Fatalf("got %+v", repos)
	}
}

func TestDuplicateRemotesGetDistinctKeys(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"a", "b"} {
		r := testutil.Repo(t, filepath.Join(root, d), "git@github.com:me/same.git")
		testutil.Write(t, filepath.Join(r, ".env"), "A=1\n")
	}
	repos, err := discover.Scan([]string{root}, opts())
	if err != nil {
		t.Fatal(err)
	}
	if repos[0].Key == repos[1].Key {
		t.Fatalf("keys collide: %q", repos[0].Key)
	}
	if repos[0].Key != "github.com/me/same@a" {
		t.Fatalf("key = %q", repos[0].Key)
	}
}
