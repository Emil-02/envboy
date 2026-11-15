package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Emil-02/envboy/internal/testutil"
)

// run executes the CLI in-process and returns stdout and the exit code.
func run(t *testing.T, args ...string) (string, int) {
	t.Helper()
	gf = globalFlags{}
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	root := NewRoot()
	root.SetArgs(append([]string{"--no-color"}, args...))
	runErr := root.Execute()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	code := 0
	if runErr != nil {
		var ec exitCode
		if errors.As(runErr, &ec) {
			code = int(ec)
		} else {
			t.Logf("error: %v", runErr)
			code = 1
		}
	}
	return buf.String(), code
}

func setup(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ENVBOY_CONFIG_DIR", filepath.Join(dir, "config"))
	t.Setenv("ENVBOY_PASSPHRASE", "e2e passphrase")
	t.Setenv("NO_COLOR", "1")
	return dir
}

// TestBackupRestoreOnNewMachine is the main story: back up every repo, lose
// the laptop, clone the repos into a different layout, restore.
func TestBackupRestoreOnNewMachine(t *testing.T) {
	dir := setup(t)
	old := filepath.Join(dir, "old")
	web := testutil.Repo(t, filepath.Join(old, "Aplikasi", "Web"), "git@github.com:Me/Web.git")
	testutil.Write(t, filepath.Join(web, ".gitignore"), ".env*\n!.env.example\n")
	testutil.Write(t, filepath.Join(web, ".env"), "DB=postgres://secret\n")
	testutil.Write(t, filepath.Join(web, "server/.env.local"), "TOKEN=abc\n")
	testutil.Write(t, filepath.Join(web, ".env.example"), "DB=\n")
	testutil.Write(t, filepath.Join(old, "notes", ".env"), "NOTE=1\n")
	vaultPath := filepath.Join(dir, "envboy.vault")

	if out, code := run(t, "backup", old, "--vault", vaultPath); code != 0 || !strings.Contains(out, "Backed up 3 files from 2 repos") {
		t.Fatalf("backup: code %d\n%s", code, out)
	}
	raw := testutil.Read(t, vaultPath)
	if strings.Contains(raw, "postgres://secret") || strings.Contains(raw, "TOKEN") {
		t.Fatal("vault is not encrypted")
	}

	// New machine: different folder names, HTTPS remote instead of SSH.
	fresh := filepath.Join(dir, "new")
	newWeb := testutil.Repo(t, filepath.Join(fresh, "code", "web-app"), "https://github.com/me/web")
	if err := os.MkdirAll(filepath.Join(fresh, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}

	out, code := run(t, "restore", fresh, "--vault", vaultPath, "--dry-run", "--json")
	if code != 0 {
		t.Fatalf("dry run: %d %s", code, out)
	}
	var plan struct {
		Items []struct{ Action, File string }
	}
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if len(plan.Items) != 3 {
		t.Fatalf("plan = %+v", plan)
	}
	for _, it := range plan.Items {
		if it.Action != "create" {
			t.Fatalf("plan item %+v", it)
		}
	}
	if _, err := os.Stat(filepath.Join(newWeb, ".env")); err == nil {
		t.Fatal("dry run wrote files")
	}

	if out, code := run(t, "restore", fresh, "--vault", vaultPath, "--yes"); code != 0 {
		t.Fatalf("restore: %d\n%s", code, out)
	}
	if testutil.Read(t, filepath.Join(newWeb, ".env")) != "DB=postgres://secret\n" ||
		testutil.Read(t, filepath.Join(newWeb, "server", ".env.local")) != "TOKEN=abc\n" ||
		testutil.Read(t, filepath.Join(fresh, "notes", ".env")) != "NOTE=1\n" {
		t.Fatal("restored content mismatch")
	}

	if out, code := run(t, "status", fresh, "--vault", vaultPath); code != 0 || !strings.Contains(out, "in sync") {
		t.Fatalf("status after restore: %d\n%s", code, out)
	}
	testutil.Write(t, filepath.Join(newWeb, ".env"), "DB=changed\nNEW_KEY=1\n")
	out, code = run(t, "status", fresh, "--vault", vaultPath)
	if code != 3 || !strings.Contains(out, "changed") || !strings.Contains(out, "NEW_KEY") || strings.Contains(out, "postgres") {
		t.Fatalf("status after edit: %d\n%s", code, out)
	}

	// A backup from the new machine merges: nothing from "old" is lost.
	if out, code := run(t, "backup", filepath.Join(fresh, "code"), "--vault", vaultPath); code != 0 || !strings.Contains(out, "kept 1 file") {
		t.Fatalf("merge backup: %d\n%s", code, out)
	}

	t.Setenv("ENVBOY_PASSPHRASE", "wrong")
	if _, code := run(t, "restore", fresh, "--vault", vaultPath, "-n"); code == 0 {
		t.Fatal("wrong passphrase should fail")
	}
}

func TestKeygenModeNeedsNoPassphrase(t *testing.T) {
	dir := setup(t)
	t.Setenv("ENVBOY_PASSPHRASE", "")
	if out, code := run(t, "keygen"); code != 0 || !strings.Contains(out, "age1") {
		t.Fatalf("keygen: %d %s", code, out)
	}
	repo := testutil.Repo(t, filepath.Join(dir, "p", "r"), "https://github.com/me/r")
	testutil.Write(t, filepath.Join(repo, ".env"), "A=1\n")
	v := filepath.Join(dir, "k.vault")
	if out, code := run(t, "backup", filepath.Join(dir, "p"), "--vault", v); code != 0 || !strings.Contains(out, "age keys") {
		t.Fatalf("backup with key: %d %s", code, out)
	}
	if err := os.Remove(filepath.Join(repo, ".env")); err != nil {
		t.Fatal(err)
	}
	if out, code := run(t, "restore", filepath.Join(dir, "p"), "--vault", v, "-y"); code != 0 {
		t.Fatalf("restore with key: %d %s", code, out)
	}
	if testutil.Read(t, filepath.Join(repo, ".env")) != "A=1\n" {
		t.Fatal("not restored")
	}
}

func TestScanAndCheckExitCodes(t *testing.T) {
	dir := setup(t)
	repo := testutil.Repo(t, filepath.Join(dir, "r"), "")
	testutil.Write(t, filepath.Join(repo, ".env"), "A=1\n")
	testutil.Write(t, filepath.Join(repo, ".env.example"), "A=\nB=\n")

	out, code := run(t, "scan", dir)
	if code != 0 || !strings.Contains(out, "not ignored") {
		t.Fatalf("scan: %d\n%s", code, out)
	}
	out, code = run(t, "check", dir)
	if code != 1 || !strings.Contains(out, "missing: B") {
		t.Fatalf("check: %d\n%s", code, out)
	}
	testutil.Write(t, filepath.Join(repo, ".env"), "A=1\nB=2\n")
	if _, code := run(t, "check", dir); code != 0 {
		t.Fatalf("check after fix: %d", code)
	}

	testutil.Git(t, repo, "add", ".env")
	testutil.Git(t, repo, "commit", "-qm", "oops")
	if _, code := run(t, "scan", dir); code != 2 {
		t.Fatalf("scan with a committed env file should exit 2, got %d", code)
	}
	if _, code := run(t, "guard", "fix", dir, "--untrack"); code != 0 {
		t.Fatal("guard fix failed")
	}
	if out, code := run(t, "scan", dir); code != 0 || !strings.Contains(out, "Every env file is ignored") {
		t.Fatalf("scan after fix: %d\n%s", code, out)
	}
}
