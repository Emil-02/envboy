package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Emil-02/envboy/internal/discover"
	"github.com/Emil-02/envboy/internal/keys"
	"github.com/Emil-02/envboy/internal/report"
	"github.com/Emil-02/envboy/internal/restore"
	"github.com/Emil-02/envboy/internal/vault"
)

func openVault() (*vault.Vault, *keys.Unlocker, error) {
	unlock, err := keySource().Unlocker()
	if err != nil {
		return nil, nil, err
	}
	v, err := vault.Read(cfg.Vault, unlock.Identities())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, fmt.Errorf("no vault at %s (run envboy backup first, or pass --vault)", shortPath(cfg.Vault))
	}
	if err != nil {
		return nil, nil, fmt.Errorf("open %s: %w", shortPath(cfg.Vault), err)
	}
	return v, unlock, nil
}

func filterVault(v *vault.Vault, only []string) {
	if len(only) == 0 {
		return
	}
	var kept []vault.Entry
	for _, e := range v.Entries {
		for _, o := range only {
			o = strings.ToLower(o)
			if strings.Contains(strings.ToLower(e.Repo), o) || strings.Contains(strings.ToLower(e.Path), o) {
				kept = append(kept, e)
				break
			}
		}
	}
	v.Entries = kept
}

func newRestoreCmd() *cobra.Command {
	var (
		dryRun, force, backupExisting, yes bool
		only                               []string
	)
	c := &cobra.Command{
		Use:   "restore [dir...]",
		Short: "Put env files from the vault back into your repos",
		Long: `Decrypts the vault and writes each env file into the matching local repo.

Repos are matched by git remote, so it does not matter where you cloned them on
this machine. Repos without a remote are matched by their path under the given
directories. Existing files that differ are left alone unless you pass --force
or --backup-existing. Restored files get 0600 permissions.`,
		Example: `  envboy restore ~/Proyek --dry-run
  envboy restore ~/Proyek --only campus_hub
  envboy restore ~/code --backup-existing`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if force && backupExisting {
				return errors.New("--force and --backup-existing are mutually exclusive")
			}
			v, _, err := openVault()
			if err != nil {
				return err
			}
			filterVault(v, only)
			rs := roots(args)
			opt := scanOptions()
			opt.AllRepos = true
			local, err := discover.Scan(rs, opt)
			if err != nil {
				return err
			}
			plan := restore.Build(v, local, rs)
			if gf.jsonOut && dryRun {
				return report.JSON(os.Stdout, plan)
			}
			if !gf.jsonOut {
				printPlan(plan, force, backupExisting)
			}
			toWrite := plan.Count(restore.Create)
			if force || backupExisting {
				toWrite += plan.Count(restore.Conflict)
			}
			if dryRun || toWrite == 0 {
				if toWrite == 0 && !gf.jsonOut {
					fmt.Println(report.Faint.Render("Nothing to write."))
				}
				return nil
			}
			if !yes && !confirm(fmt.Sprintf("Write %s?", report.Plural(toWrite, "file", "files")), false) {
				if !stdinIsTerminal() {
					return errors.New("not a terminal: pass --yes to write files")
				}
				return exitCode(1)
			}
			res := restore.Apply(plan, restore.Options{Force: force, BackupExisting: backupExisting})
			if gf.jsonOut {
				return report.JSON(os.Stdout, map[string]any{
					"written": len(res.Written), "backed_up": res.BackedUp,
					"skipped": len(res.Skipped), "errors": errStrings(res.Errors),
				})
			}
			for _, b := range res.BackedUp {
				fmt.Println(report.Faint.Render("  moved aside: " + shortPath(b)))
			}
			for _, e := range res.Errors {
				fmt.Fprintln(os.Stderr, report.Bad.Render("error:"), e)
			}
			fmt.Printf("%s Restored %s\n", report.OK.Render("✓"), report.Plural(len(res.Written), "file", "files"))
			if len(res.Errors) > 0 {
				return exitCode(1)
			}
			return nil
		},
	}
	f := c.Flags()
	f.BoolVarP(&dryRun, "dry-run", "n", false, "show the plan without writing")
	f.BoolVarP(&force, "force", "f", false, "overwrite local files that differ from the vault")
	f.BoolVarP(&backupExisting, "backup-existing", "b", false, "rename differing local files to .bak, then write")
	f.BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	f.StringArrayVar(&only, "only", nil, "only restore repos whose key or path contains this text (repeatable)")
	return c
}

func errStrings(errs []error) []string {
	out := []string{}
	for _, e := range errs {
		out = append(out, e.Error())
	}
	return out
}

func actionLabel(a restore.Action, overwrite bool) string {
	switch a {
	case restore.Create:
		return report.OK.Render("+ create")
	case restore.Identical:
		return report.Faint.Render("= same")
	case restore.Conflict:
		if overwrite {
			return report.Warn.Render("~ overwrite")
		}
		return report.Warn.Render("! differs")
	case restore.NoRepo:
		return report.Faint.Render("? no repo")
	case restore.Ambiguous:
		return report.Warn.Render("? ambiguous")
	case restore.Invalid:
		return report.Bad.Render("✗ invalid")
	}
	return string(a)
}

func itemNote(it restore.Item) string {
	var parts []string
	if it.RepoDir != "" {
		parts = append(parts, shortPath(it.RepoDir))
	}
	if it.Detail != "" {
		parts = append(parts, it.Detail)
	}
	if len(it.VaultOnly) > 0 {
		parts = append(parts, "vault has "+report.List(it.VaultOnly, 4))
	}
	if len(it.LocalOnly) > 0 {
		parts = append(parts, "local has "+report.List(it.LocalOnly, 4))
	}
	return strings.Join(parts, "\n")
}

func printPlan(plan restore.Plan, force, backupExisting bool) {
	var rows [][]string
	for _, it := range plan.Items {
		rows = append(rows, []string{actionLabel(it.Action, force || backupExisting), it.Repo, it.File, itemNote(it)})
	}
	if len(rows) == 0 {
		fmt.Println(report.Faint.Render("The vault is empty."))
		return
	}
	report.Table(os.Stdout, []string{"Action", "Repo", "File", "Where / notes"}, rows)
	fmt.Printf("%d to create · %d identical · %d differ · %d without a local repo\n",
		plan.Count(restore.Create), plan.Count(restore.Identical), plan.Count(restore.Conflict),
		plan.Count(restore.NoRepo)+plan.Count(restore.Ambiguous))
	if c := plan.Count(restore.Conflict); c > 0 && !force && !backupExisting {
		fmt.Println(report.Faint.Render("Differing files are kept. Use --backup-existing or --force to replace them."))
	}
}
