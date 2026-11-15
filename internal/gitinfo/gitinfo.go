// Package gitinfo wraps the few git plumbing calls envboy needs.
// Every function degrades gracefully when git is not installed.
package gitinfo

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"path"
	"strings"
	"sync"
)

var (
	availOnce sync.Once
	avail     bool
)

// Available reports whether a git binary is on PATH.
func Available() bool {
	availOnce.Do(func() {
		_, err := exec.LookPath("git")
		avail = err == nil
	})
	return avail
}

func run(dir string, stdin []byte, args ...string) ([]byte, error) {
	if !Available() {
		return nil, errors.New("git not found in PATH")
	}
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return out, &ExitError{Code: ee.ExitCode(), Stderr: strings.TrimSpace(stderr.String())}
		}
		return out, err
	}
	return out, nil
}

// ExitError is a non-zero git exit.
type ExitError struct {
	Code   int
	Stderr string
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("git exited with %d: %s", e.Code, e.Stderr)
}

// Remote returns the URL of "origin", or of the first remote when there is no origin.
func Remote(repoDir string) string {
	if out, err := run(repoDir, nil, "remote", "get-url", "origin"); err == nil {
		return strings.TrimSpace(string(out))
	}
	out, err := run(repoDir, nil, "remote")
	if err != nil {
		return ""
	}
	first := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if first == "" {
		return ""
	}
	if out, err := run(repoDir, nil, "remote", "get-url", first); err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}

// NormalizeRemote turns any remote URL form into "host/owner/repo" (lower
// case, no scheme, credentials, port or .git suffix) so that the SSH and
// HTTPS URLs of the same repository compare equal.
func NormalizeRemote(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	var host, p string
	switch {
	case strings.Contains(s, "://"):
		u, err := url.Parse(s)
		if err != nil {
			return strings.ToLower(strings.TrimSuffix(s, ".git"))
		}
		if u.Scheme == "file" {
			return "file:" + path.Clean(u.Path)
		}
		host, p = u.Hostname(), u.Path
	case isSCP(s):
		// user@host:owner/repo
		at := strings.Index(s, "@")
		colon := strings.Index(s, ":")
		host, p = s[at+1:colon], s[colon+1:]
	case strings.HasPrefix(s, "/") || strings.HasPrefix(s, ".") || (len(s) > 1 && s[1] == ':'):
		return "file:" + path.Clean(strings.ReplaceAll(s, `\`, "/"))
	default:
		// host:owner/repo without a user
		if i := strings.Index(s, ":"); i > 0 && !strings.Contains(s[:i], "/") {
			host, p = s[:i], s[i+1:]
		} else {
			return strings.ToLower(strings.TrimSuffix(s, ".git"))
		}
	}
	p = strings.Trim(p, "/")
	p = strings.TrimSuffix(p, ".git")
	p = strings.Trim(p, "/")
	return strings.ToLower(host + "/" + p)
}

func isSCP(s string) bool {
	at := strings.Index(s, "@")
	colon := strings.Index(s, ":")
	return at > 0 && colon > at && !strings.Contains(s[:colon], "/")
}

// Tracked returns which of paths (relative to repoDir) are tracked by git.
func Tracked(repoDir string, paths []string) (map[string]bool, error) {
	res := map[string]bool{}
	if len(paths) == 0 {
		return res, nil
	}
	out, err := run(repoDir, nil, append([]string{"ls-files", "-z", "--"}, paths...)...)
	if err != nil {
		return res, err
	}
	for _, p := range splitZ(out) {
		res[p] = true
	}
	return res, nil
}

// Ignored returns which of paths match an ignore rule, whether or not they are tracked.
func Ignored(repoDir string, paths []string) (map[string]bool, error) {
	res := map[string]bool{}
	if len(paths) == 0 {
		return res, nil
	}
	in := []byte(strings.Join(paths, "\x00") + "\x00")
	out, err := run(repoDir, in, "check-ignore", "--no-index", "-z", "--stdin")
	if err != nil {
		var ee *ExitError
		if errors.As(err, &ee) && ee.Code == 1 { // nothing ignored
			return res, nil
		}
		return res, err
	}
	for _, p := range splitZ(out) {
		res[p] = true
	}
	return res, nil
}

// HooksDir returns the absolute hooks directory, honoring core.hooksPath.
func HooksDir(repoDir string) (string, error) {
	out, err := run(repoDir, nil, "rev-parse", "--path-format=absolute", "--git-path", "hooks")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// TopLevel returns the work tree root containing dir.
func TopLevel(dir string) (string, error) {
	out, err := run(dir, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Staged lists files added, copied, modified or renamed in the index.
func Staged(repoDir string) ([]string, error) {
	out, err := run(repoDir, nil, "diff", "--cached", "--name-only", "-z", "--diff-filter=ACMR")
	if err != nil {
		return nil, err
	}
	return splitZ(out), nil
}

// Untrack removes paths from the index while keeping them on disk.
func Untrack(repoDir string, paths []string) error {
	_, err := run(repoDir, nil, append([]string{"rm", "--cached", "--quiet", "--"}, paths...)...)
	return err
}

func splitZ(b []byte) []string {
	var res []string
	for _, p := range bytes.Split(b, []byte{0}) {
		if len(p) > 0 {
			res = append(res, string(p))
		}
	}
	return res
}
