// Package guard keeps env files out of git: a pre-commit hook and .gitignore fixes.
package guard

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Emil-02/envboy/internal/discover"
	"github.com/Emil-02/envboy/internal/gitinfo"
)

const (
	// Marker identifies a hook written by envboy.
	Marker      = "# envboy guard hook"
	chainedName = "pre-commit.envboy-chained"
	ignoreMark  = "# envboy: keep env files out of git"
)

// Hook renders the pre-commit script. bin is the envboy binary used at install time;
// the script falls back to envboy on PATH if it moves.
func Hook(bin string) string {
	quoted := "'" + strings.ReplaceAll(filepath.ToSlash(bin), "'", `'\''`) + "'"
	return `#!/bin/sh
` + Marker + ` - managed by "envboy guard install", remove with "envboy guard uninstall".
ENVBOY=` + quoted + `
if [ ! -x "$ENVBOY" ]; then ENVBOY="$(command -v envboy 2>/dev/null)"; fi
if [ -n "$ENVBOY" ]; then
	"$ENVBOY" guard run || exit 1
else
	echo "envboy: binary not found, env guard skipped" >&2
fi
HOOK_DIR="$(dirname "$0")"
if [ -x "$HOOK_DIR/` + chainedName + `" ]; then
	exec "$HOOK_DIR/` + chainedName + `" "$@"
fi
`
}

// InstallResult describes what Install did.
type InstallResult struct {
	Path    string
	Chained bool // an existing hook was kept and is called after envboy
	Updated bool // an envboy hook was already there and was rewritten
}

// Install writes the pre-commit hook in repoDir without clobbering an existing hook.
func Install(repoDir, bin string) (InstallResult, error) {
	dir, err := gitinfo.HooksDir(repoDir)
	if err != nil {
		return InstallResult{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return InstallResult{}, err
	}
	hook := filepath.Join(dir, "pre-commit")
	res := InstallResult{Path: hook}
	existing, err := os.ReadFile(hook)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return res, err
	case bytes.Contains(existing, []byte(Marker)):
		res.Updated = true
	default:
		chained := filepath.Join(dir, chainedName)
		if _, err := os.Lstat(chained); err == nil {
			return res, fmt.Errorf("%s and %s both exist; merge them by hand", hook, chained)
		}
		if err := os.Rename(hook, chained); err != nil {
			return res, err
		}
		res.Chained = true
	}
	if _, err := os.Lstat(filepath.Join(dir, chainedName)); err == nil {
		res.Chained = true
	}
	return res, os.WriteFile(hook, []byte(Hook(bin)), 0o755)
}

// Uninstall removes the envboy hook and puts back a chained hook.
func Uninstall(repoDir string) (bool, error) {
	dir, err := gitinfo.HooksDir(repoDir)
	if err != nil {
		return false, err
	}
	hook := filepath.Join(dir, "pre-commit")
	data, err := os.ReadFile(hook)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !bytes.Contains(data, []byte(Marker))) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := os.Remove(hook); err != nil {
		return false, err
	}
	chained := filepath.Join(dir, chainedName)
	if _, err := os.Lstat(chained); err == nil {
		return true, os.Rename(chained, hook)
	}
	return true, nil
}

// Installed reports whether repoDir has the envboy hook.
func Installed(repoDir string) bool {
	dir, err := gitinfo.HooksDir(repoDir)
	if err != nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(dir, "pre-commit"))
	return err == nil && bytes.Contains(data, []byte(Marker))
}

// StagedEnvFiles returns staged files that are env files (not templates).
func StagedEnvFiles(repoDir string, opt discover.Options) ([]string, error) {
	staged, err := gitinfo.Staged(repoDir)
	if err != nil {
		return nil, err
	}
	var bad []string
	for _, p := range staged {
		if opt.Classify(filepath.Base(p)) == discover.KindEnv {
			bad = append(bad, p)
		}
	}
	return bad, nil
}

// FixResult describes what FixIgnore changed.
type FixResult struct {
	GitignoreLines []string
	Untracked      []string
}

// Fix makes every env file in r ignored, and with untrack also removes
// tracked ones from the index (the files stay on disk).
func Fix(r discover.Repo, markers []string, untrack bool) (FixResult, error) {
	var res FixResult
	if !r.IsGit {
		return res, nil
	}
	var unignored, tracked []string
	for _, f := range r.EnvFiles {
		if !f.Ignored {
			unignored = append(unignored, f.Rel)
		}
		if f.Tracked {
			tracked = append(tracked, f.Rel)
		}
	}
	gi := filepath.Join(r.Dir, ".gitignore")
	if len(unignored) > 0 {
		current, err := os.ReadFile(gi)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return res, err
		}
		var lines []string
		if !bytes.Contains(current, []byte(ignoreMark)) {
			lines = append(lines, ignoreMark, ".env", ".env.*")
			for _, m := range markers {
				lines = append(lines, "!*.env."+m, "!.env.*."+m)
			}
		}
		if err := appendLines(gi, current, lines); err != nil {
			return res, err
		}
		res.GitignoreLines = append(res.GitignoreLines, lines...)

		// Anything the generic block does not cover gets an explicit entry.
		still, err := gitinfo.Ignored(r.Dir, unignored)
		if err != nil {
			return res, err
		}
		var explicit []string
		for _, p := range unignored {
			if !still[p] {
				explicit = append(explicit, "/"+p)
			}
		}
		if len(explicit) > 0 {
			current, _ := os.ReadFile(gi)
			if !bytes.Contains(current, []byte(ignoreMark)) {
				explicit = append([]string{ignoreMark}, explicit...)
			}
			if err := appendLines(gi, current, explicit); err != nil {
				return res, err
			}
			res.GitignoreLines = append(res.GitignoreLines, explicit...)
		}
	}
	if untrack && len(tracked) > 0 {
		if err := gitinfo.Untrack(r.Dir, tracked); err != nil {
			return res, err
		}
		res.Untracked = tracked
	}
	return res, nil
}

func appendLines(path string, current []byte, lines []string) error {
	if len(lines) == 0 {
		return nil
	}
	var b bytes.Buffer
	if len(current) > 0 && !bytes.HasSuffix(current, []byte("\n")) {
		b.WriteByte('\n')
	}
	if len(current) > 0 {
		b.WriteByte('\n')
	}
	b.WriteString(strings.Join(lines, "\n"))
	b.WriteByte('\n')
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(b.Bytes()); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
