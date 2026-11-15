package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Emil-02/envboy/internal/discover"
	"github.com/Emil-02/envboy/internal/gitinfo"
	"github.com/Emil-02/envboy/internal/guard"
	"github.com/Emil-02/envboy/internal/report"
)

func newGuardCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "guard",
		Short: "Keep env files out of git (pre-commit hook and .gitignore fixes)",
	}
	c.AddCommand(newGuardInstallCmd(), newGuardUninstallCmd(), newGuardRunCmd(), newGuardFixCmd())
	return c
}

// guardTargets returns the repos to act on: every git repo under dirs with
// --all, otherwise the repo containing each dir (or the working directory).
func guardTargets(args []string, all bool) ([]string, error) {
	if all {
		opt := scanOptions()
		opt.AllRepos = true
		repos, err := discover.Scan(roots(args), opt)
		if err != nil {
			return nil, err
		}
		var dirs []string
		for _, r := range repos {
			if r.IsGit {
				dirs = append(dirs, r.Dir)
			}
		}
		return dirs, nil
	}
	if len(args) == 0 {
		args = []string{"."}
	}
	var dirs []string
	for _, a := range args {
		top, err := gitinfo.TopLevel(a)
		if err != nil {
			return nil, fmt.Errorf("%s is not inside a git repository (use --all to install in every repo below it)", a)
		}
		dirs = append(dirs, top)
	}
	return dirs, nil
}

func newGuardInstallCmd() *cobra.Command {
	var all bool
	c := &cobra.Command{
		Use:   "install [dir...]",
		Short: "Install a pre-commit hook that blocks committing env files",
		Long: `Installs a pre-commit hook that runs "envboy guard run". An existing pre-commit
hook is kept and called after envboy's check. core.hooksPath is honored.`,
		Example: `  envboy guard install
  envboy guard install --all ~/Proyek`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs, err := guardTargets(args, all)
			if err != nil {
				return err
			}
			bin, err := os.Executable()
			if err != nil {
				bin = "envboy"
			}
			if resolved, err := filepath.EvalSymlinks(bin); err == nil {
				bin = resolved
			}
			failed := 0
			for _, d := range dirs {
				res, err := guard.Install(d, bin)
				if err != nil {
					failed++
					fmt.Fprintln(os.Stderr, report.Bad.Render("✗"), shortPath(d)+":", err)
					continue
				}
				note := ""
				switch {
				case res.Updated:
					note = " (updated)"
				case res.Chained:
					note = " (existing hook kept, runs after envboy)"
				}
				fmt.Println(report.OK.Render("✓"), shortPath(d)+report.Faint.Render(note))
			}
			if failed > 0 {
				return exitCode(1)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&all, "all", false, "install in every git repo found under the given directories")
	return c
}

func newGuardUninstallCmd() *cobra.Command {
	var all bool
	c := &cobra.Command{
		Use:   "uninstall [dir...]",
		Short: "Remove the envboy pre-commit hook (restores a chained hook)",
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs, err := guardTargets(args, all)
			if err != nil {
				return err
			}
			for _, d := range dirs {
				removed, err := guard.Uninstall(d)
				switch {
				case err != nil:
					fmt.Fprintln(os.Stderr, report.Bad.Render("✗"), shortPath(d)+":", err)
				case removed:
					fmt.Println(report.OK.Render("✓ removed"), shortPath(d))
				case !all:
					fmt.Println(report.Faint.Render("no envboy hook in " + shortPath(d)))
				}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&all, "all", false, "uninstall from every git repo found under the given directories")
	return c
}

func newGuardRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Fail if staged changes include an env file (used by the hook)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			top, err := gitinfo.TopLevel(".")
			if err != nil {
				return errors.New("not inside a git repository")
			}
			bad, err := guard.StagedEnvFiles(top, scanOptions())
			if err != nil {
				return err
			}
			if len(bad) == 0 {
				return nil
			}
			fmt.Fprintln(os.Stderr, report.Bad.Render("envboy: refusing to commit env files:"))
			for _, b := range bad {
				fmt.Fprintln(os.Stderr, "  "+b)
			}
			fmt.Fprintln(os.Stderr, report.Faint.Render(`Unstage them:  git restore --staged <file>
Ignore them:   envboy guard fix
Commit anyway: git commit --no-verify`))
			return exitCode(1)
		},
	}
}

func newGuardFixCmd() *cobra.Command {
	var untrack bool
	c := &cobra.Command{
		Use:   "fix [dir...]",
		Short: "Add env files to .gitignore (and optionally untrack committed ones)",
		Long: `Appends ignore rules to .gitignore in every repo whose env files are not ignored.
Templates such as .env.example stay committable.

With --untrack, env files that are already committed are removed from the index
(git rm --cached); the files stay on disk. Their secrets are still in git history,
so rotate them.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			repos, err := discover.Scan(roots(args), scanOptions())
			if err != nil {
				return err
			}
			changed := 0
			for _, r := range repos {
				res, err := guard.Fix(r, cfg.TemplateMarkers, untrack)
				if err != nil {
					fmt.Fprintln(os.Stderr, report.Bad.Render("✗"), r.Name()+":", err)
					continue
				}
				if len(res.GitignoreLines) > 0 {
					changed++
					fmt.Println(report.OK.Render("✓"), r.Name(), report.Faint.Render("updated .gitignore"))
				}
				if len(res.Untracked) > 0 {
					changed++
					fmt.Println(report.Warn.Render("⚠"), r.Name(), "untracked", report.List(res.Untracked, 5),
						report.Faint.Render("(commit this, and rotate those secrets)"))
				}
				if !untrack {
					for _, f := range r.EnvFiles {
						if f.Tracked {
							fmt.Println(report.Bad.Render("✗"), r.Name(), f.Rel, report.Faint.Render("is committed: rerun with --untrack"))
						}
					}
				}
			}
			if changed == 0 {
				fmt.Println(report.OK.Render("✓ Nothing to fix."))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&untrack, "untrack", false, "also git rm --cached env files that are committed")
	return c
}
