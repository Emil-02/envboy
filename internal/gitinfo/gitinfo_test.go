package gitinfo_test

import (
	"path/filepath"
	"testing"

	"github.com/Emil-02/envboy/internal/gitinfo"
	"github.com/Emil-02/envboy/internal/testutil"
)

func TestNormalizeRemote(t *testing.T) {
	cases := map[string]string{
		"git@github.com:Emil-02/envboy.git":                   "github.com/emil-02/envboy",
		"https://github.com/Emil-02/envboy":                   "github.com/emil-02/envboy",
		"https://github.com/Emil-02/envboy.git/":              "github.com/emil-02/envboy",
		"https://user:ghp_secret@github.com/Emil-02/envboy":   "github.com/emil-02/envboy",
		"ssh://git@github.com:22/Emil-02/envboy.git":          "github.com/emil-02/envboy",
		"git://gitlab.example.com/group/sub/project.git":      "gitlab.example.com/group/sub/project",
		"github.com:Emil-02/envboy":                           "github.com/emil-02/envboy",
		"/srv/git/project.git":                                "file:/srv/git/project.git",
		"file:///srv/git/project":                             "file:/srv/git/project",
		"":                                                    "",
		"  git@bitbucket.org:Team/Repo.git  ":                 "bitbucket.org/team/repo",
		"https://dev.azure.com/org/proj/_git/repo":            "dev.azure.com/org/proj/_git/repo",
		"git@ssh.dev.azure.com:v3/org/proj/repo":              "ssh.dev.azure.com/v3/org/proj/repo",
		"https://gitea.local:3000/owner/repo.git?query=strip": "gitea.local/owner/repo",
	}
	for in, want := range cases {
		if got := gitinfo.NormalizeRemote(in); got != want {
			t.Errorf("NormalizeRemote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTrackedIgnoredRemote(t *testing.T) {
	dir := testutil.Repo(t, filepath.Join(t.TempDir(), "r"), "git@github.com:Me/R.git")
	testutil.Write(t, filepath.Join(dir, ".gitignore"), ".env\n")
	testutil.Write(t, filepath.Join(dir, ".env"), "A=1\n")
	testutil.Write(t, filepath.Join(dir, ".env.local"), "A=1\n")
	testutil.Git(t, dir, "add", ".gitignore", ".env.local")
	testutil.Git(t, dir, "commit", "-qm", "init")

	if got := gitinfo.Remote(dir); got != "git@github.com:Me/R.git" {
		t.Fatalf("Remote = %q", got)
	}
	tracked, err := gitinfo.Tracked(dir, []string{".env", ".env.local"})
	if err != nil {
		t.Fatal(err)
	}
	if tracked[".env"] || !tracked[".env.local"] {
		t.Fatalf("tracked = %v", tracked)
	}
	ignored, err := gitinfo.Ignored(dir, []string{".env", ".env.local"})
	if err != nil {
		t.Fatal(err)
	}
	if !ignored[".env"] || ignored[".env.local"] {
		t.Fatalf("ignored = %v", ignored)
	}
	none, err := gitinfo.Ignored(dir, []string{".env.local"})
	if err != nil || len(none) != 0 {
		t.Fatalf("expected nothing ignored, got %v %v", none, err)
	}
}
