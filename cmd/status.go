package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Emil-02/envboy/internal/discover"
	"github.com/Emil-02/envboy/internal/report"
	"github.com/Emil-02/envboy/internal/restore"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [dir...]",
		Short: "Compare the vault with the env files on disk",
		Long: `Shows which env files changed since the last backup, which are missing on disk,
and which local files are not in the vault yet. Only key names are shown, never values.

Exits with 3 when the vault and the disk differ, so it can be used in scripts.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			v, _, err := openVault()
			if err != nil {
				return err
			}
			rs := roots(args)
			opt := scanOptions()
			opt.AllRepos = true
			local, err := discover.Scan(rs, opt)
			if err != nil {
				return err
			}
			st := restore.BuildStatus(v, local, rs)
			if gf.jsonOut {
				return report.JSON(os.Stdout, st)
			}
			fmt.Printf("%s %s · %s · updated %s on %s\n", report.Title.Render("Vault"),
				shortPath(cfg.Vault), report.Plural(len(v.Entries), "file", "files"),
				v.UpdatedAt.Local().Format("2006-01-02 15:04"), v.Hostname)

			var rows [][]string
			for _, it := range st.Plan.Items {
				var label string
				switch it.Action {
				case restore.Identical:
					continue
				case restore.Create:
					label = report.Warn.Render("missing on disk")
				case restore.Conflict:
					label = report.Info.Render("changed")
				case restore.Invalid:
					label = report.Bad.Render("invalid")
				default:
					continue // repos that live on another machine are normal
				}
				rows = append(rows, []string{label, it.Repo, it.File, itemNote(it)})
			}
			for _, f := range st.NotBackedUp {
				rows = append(rows, []string{report.Accent.Render("not backed up"), f.Repo, f.File, shortPath(f.Dir)})
			}
			elsewhere := st.Plan.Count(restore.NoRepo) + st.Plan.Count(restore.Ambiguous)
			if len(rows) == 0 {
				fmt.Println(report.OK.Render("✓ Vault and disk are in sync."))
				if elsewhere > 0 {
					fmt.Println(report.Faint.Render(fmt.Sprintf("  %s belong to repos not cloned here.", report.Plural(elsewhere, "file", "files"))))
				}
				return nil
			}
			report.Table(os.Stdout, []string{"State", "Repo", "File", "Notes"}, rows)
			fmt.Printf("%d in sync · %d changed · %d missing on disk · %d not backed up · %d for repos not here\n",
				st.Plan.Count(restore.Identical), st.Plan.Count(restore.Conflict),
				st.Plan.Count(restore.Create), len(st.NotBackedUp), elsewhere)
			if st.Plan.Count(restore.Conflict)+len(st.NotBackedUp) > 0 {
				fmt.Println(report.Faint.Render("Run envboy backup to update the vault."))
			}
			return exitCode(3)
		},
	}
}
