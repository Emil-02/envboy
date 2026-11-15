package guard_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/Emil-02/envboy/internal/config"
	"github.com/Emil-02/envboy/internal/discover"
	"github.com/Emil-02/envboy/internal/guard"
	"github.com/Emil-02/envboy/internal/testutil"
)

func opts() discover.Options { return discover.FromConfig(config.Default()) }

func TestInstallChainsAndUninstallRestores(t *testing.T) {
	repo := testutil.Repo(t, filepath.Join(t.TempDir(), "r"), "")
	hooks := filepath.Join(repo, ".git", "hooks")
	old := "#!/bin/sh\necho old\n"
	testutil.Write(t, filepath.Join(hooks, "pre-commit"), old)

	res, err := guard.Install(repo, "/opt/it's/envboy")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Chained || res.Updated {
		t.Fatalf("first install = %+v", res)
	}
	hook := testutil.Read(t, filepath.Join(hooks, "pre-commit"))
	if !strings.Contains(hook, guard.Marker) || !strings.Contains(hook, `ENVBOY='/opt/it'\''s/envboy'`) {
		t.Fatalf("hook content:\n%s", hook)
	}
	if st, _ := os.Stat(filepath.Join(hooks, "pre-commit")); runtime.GOOS != "windows" && st.Mode().Perm()&0o100 == 0 {
		t.Fatal("hook is not executable")
	}
	if testutil.Read(t, filepath.Join(hooks, "pre-commit.envboy-chained")) != old {
		t.Fatal("old hook not preserved")
	}
	if !guard.Installed(repo) {
		t.Fatal("Installed = false")
	}

	res, err = guard.Install(repo, "envboy")
	if err != nil || !res.Updated || !res.Chained {
		t.Fatalf("reinstall = %+v, %v", res, err)
	}

	removed, err := guard.Uninstall(repo)
	if err != nil || !removed {
		t.Fatalf("uninstall = %v, %v", removed, err)
	}
	if testutil.Read(t, filepath.Join(hooks, "pre-commit")) != old {
		t.Fatal("old hook not restored")
	}
}

func TestHooksPathHonored(t *testing.T) {
	repo := testutil.Repo(t, filepath.Join(t.TempDir(), "r"), "")
	testutil.Git(t, repo, "config", "core.hooksPath", ".githooks")
	res, err := guard.Install(repo, "envboy")
	if err != nil {
		t.Fatal(err)
	}
	// Compare by identity: temp dirs can be symlinked (macOS) or short names (Windows).
	got, err1 := os.Stat(filepath.Dir(res.Path))
	want, err2 := os.Stat(filepath.Join(repo, ".githooks"))
	if err1 != nil || err2 != nil || !os.SameFile(got, want) || filepath.Base(res.Path) != "pre-commit" {
		t.Fatalf("hook path = %s", res.Path)
	}
}

func TestStagedEnvFiles(t *testing.T) {
	repo := testutil.Repo(t, filepath.Join(t.TempDir(), "r"), "")
	testutil.Write(t, filepath.Join(repo, ".env"), "A=1\n")
	testutil.Write(t, filepath.Join(repo, ".env.example"), "A=\n")
	testutil.Write(t, filepath.Join(repo, "cfg/prod.env"), "A=1\n")
	testutil.Write(t, filepath.Join(repo, "main.go"), "package main\n")
	testutil.Git(t, repo, "add", "-A")
	bad, err := guard.StagedEnvFiles(repo, opts())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bad, []string{".env", "cfg/prod.env"}) {
		t.Fatalf("bad = %v", bad)
	}
}

func TestHookBlocksCommit(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	// A fake envboy that always refuses proves the hook wiring and exit code.
	dir := t.TempDir()
	fake := filepath.Join(dir, "envboy")
	testutil.Write(t, fake, "#!/bin/sh\nexit 1\n")
	if err := os.Chmod(fake, 0o755); err != nil {
		t.Fatal(err)
	}
	repo := testutil.Repo(t, filepath.Join(dir, "r"), "")
	if _, err := guard.Install(repo, fake); err != nil {
		t.Fatal(err)
	}
	testutil.Write(t, filepath.Join(repo, "a.txt"), "x")
	testutil.Git(t, repo, "add", "a.txt")
	cmd := exec.Command("git", "-C", repo, "-c", "user.name=t", "-c", "user.email=t@e", "commit", "-qm", "x")
	if err := cmd.Run(); err == nil {
		t.Fatal("commit should have been blocked by the hook")
	}
}

func TestFix(t *testing.T) {
	root := t.TempDir()
	repo := testutil.Repo(t, filepath.Join(root, "r"), "")
	testutil.Write(t, filepath.Join(repo, ".gitignore"), "node_modules") // no trailing newline
	testutil.Write(t, filepath.Join(repo, ".env"), "A=1\n")
	testutil.Write(t, filepath.Join(repo, ".env.example"), "A=\n")
	testutil.Write(t, filepath.Join(repo, "deploy/prod.env"), "A=1\n")
	testutil.Write(t, filepath.Join(repo, ".env.committed"), "A=1\n")
	testutil.Git(t, repo, "add", ".env.committed", ".env.example")
	testutil.Git(t, repo, "-c", "user.name=t", "-c", "user.email=t@e", "commit", "-qm", "x")

	scan := func() discover.Repo {
		repos, err := discover.Scan([]string{root}, opts())
		if err != nil || len(repos) != 1 {
			t.Fatalf("scan: %v %+v", err, repos)
		}
		return repos[0]
	}
	res, err := guard.Fix(scan(), config.DefaultTemplateMarkers, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Untracked, []string{".env.committed"}) {
		t.Fatalf("untracked = %v", res.Untracked)
	}
	gi := testutil.Read(t, filepath.Join(repo, ".gitignore"))
	if !strings.HasPrefix(gi, "node_modules\n\n# envboy") || !strings.Contains(gi, "/deploy/prod.env") {
		t.Fatalf(".gitignore:\n%s", gi)
	}
	r := scan()
	for _, f := range r.EnvFiles {
		if r.Risk(f) != discover.RiskNone {
			t.Errorf("%s still at risk: %s", f.Rel, r.Risk(f))
		}
	}
	for _, f := range r.Templates {
		if f.Ignored {
			t.Errorf("template %s must stay committable", f.Rel)
		}
	}

	again, err := guard.Fix(r, config.DefaultTemplateMarkers, true)
	if err != nil || len(again.GitignoreLines) != 0 || len(again.Untracked) != 0 {
		t.Fatalf("second fix should be a no-op: %+v %v", again, err)
	}
}
