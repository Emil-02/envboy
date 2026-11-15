package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Emil-02/envboy/internal/backup"
	"github.com/Emil-02/envboy/internal/discover"
	"github.com/Emil-02/envboy/internal/keys"
	"github.com/Emil-02/envboy/internal/report"
	"github.com/Emil-02/envboy/internal/vault"
)

func newBackupCmd() *cobra.Command {
	var (
		recipients     []string
		recipientFiles []string
		usePassphrase  bool
		prune          bool
		binary         bool
		dryRun         bool
	)
	c := &cobra.Command{
		Use:   "backup [dir...]",
		Short: "Encrypt every env file into one vault",
		Long: `Collects every env file under the given directories into a single age-encrypted
vault (default ~/envboy.vault). Templates such as .env.example are skipped.

If the vault already exists it is updated: repos found in this scan are replaced,
repos that are not on this machine are kept. Use --prune to drop them.

Encryption: a passphrase (ENVBOY_PASSPHRASE or a prompt), or age keys from
--recipient, --recipients-file, the config file, or the identity created by
"envboy keygen".`,
		Example: `  envboy backup ~/Proyek
  envboy backup ~/Proyek --vault /media/usb/envboy.vault
  envboy backup -r age1qyqszqgpqyqszqgpqyqszqgpqyqszqgpqyqszqgpqyqszqgpqyqs3290gq`,
		RunE: func(cmd *cobra.Command, args []string) error {
			repos, err := discover.Scan(roots(args), scanOptions())
			if err != nil {
				return err
			}
			entries, scanned, errs := backup.Collect(repos)
			for _, e := range errs {
				fmt.Fprintln(os.Stderr, report.Warn.Render("skip:"), e)
			}
			if dryRun {
				printEntries(entries)
				return nil
			}
			if len(entries) == 0 && !vaultExists() {
				fmt.Fprintln(os.Stderr, report.Faint.Render("No env files found, nothing to back up."))
				return nil
			}

			src := keySource()
			explicit := usePassphrase || len(recipients) > 0 || len(recipientFiles) > 0
			if explicit {
				src.ForcePassphrase = usePassphrase
				src.Recipients = recipients
				src.RecipientFiles = recipientFiles
			}

			v := vault.New("envboy " + Version)
			var enc *keys.Encryption
			if vaultExists() && !prune {
				unlock, err := src.Unlocker()
				if err != nil {
					return err
				}
				existing, err := vault.Read(cfg.Vault, unlock.Identities())
				if err != nil {
					return fmt.Errorf("open existing vault %s: %w\n(use --prune to overwrite it, or --vault for a new file)", shortPath(cfg.Vault), err)
				}
				v = existing
				if !explicit {
					if enc, err = unlock.Encryption(); err != nil {
						return err
					}
				}
			}
			if enc == nil {
				if enc, err = src.Encryption(); err != nil {
					return err
				}
			}
			v.Replace(entries, scanned, !prune)
			kept := len(v.Entries) - len(entries)
			if err := vault.Write(cfg.Vault, v, enc.Recipients, !binary); err != nil {
				return err
			}

			if gf.jsonOut {
				return report.JSON(os.Stdout, map[string]any{
					"vault": cfg.Vault, "files": len(entries), "repos": countRepos(entries),
					"kept_from_other_repos": kept, "encryption": enc.Mode,
				})
			}
			fmt.Printf("%s Backed up %s from %s to %s %s\n",
				report.OK.Render("✓"),
				report.Plural(len(entries), "file", "files"),
				report.Plural(countRepos(entries), "repo", "repos"),
				report.Bold.Render(shortPath(cfg.Vault)),
				report.Faint.Render("("+string(enc.Mode)+")"))
			if kept > 0 {
				fmt.Println(report.Faint.Render(fmt.Sprintf("  kept %s from repos not scanned this time (use --prune to drop)", report.Plural(kept, "file", "files"))))
			}
			if enc.Mode == keys.ModePassphrase {
				fmt.Println(report.Faint.Render("  Keep the passphrase safe: without it the vault cannot be opened."))
			}
			return nil
		},
	}
	f := c.Flags()
	f.StringArrayVarP(&recipients, "recipient", "r", nil, "encrypt to this age public key (repeatable)")
	f.StringArrayVarP(&recipientFiles, "recipients-file", "R", nil, "encrypt to the age public keys in this file (repeatable)")
	f.BoolVar(&usePassphrase, "passphrase", false, "encrypt with a passphrase even if keys are configured")
	f.BoolVar(&prune, "prune", false, "overwrite the vault instead of merging (drops repos not scanned)")
	f.BoolVar(&binary, "binary", false, "write binary age instead of ASCII armor")
	f.BoolVar(&dryRun, "dry-run", false, "list what would be backed up without writing")
	return c
}

func countRepos(entries []vault.Entry) int {
	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.Repo] = true
	}
	return len(seen)
}

func printEntries(entries []vault.Entry) {
	if gf.jsonOut {
		type row struct {
			Repo string   `json:"repo"`
			File string   `json:"file"`
			Keys []string `json:"keys"`
		}
		rows := []row{}
		for _, e := range entries {
			rows = append(rows, row{e.Repo, e.File, e.Keys})
		}
		_ = report.JSON(os.Stdout, rows)
		return
	}
	var rows [][]string
	for _, e := range entries {
		rows = append(rows, []string{e.Repo, e.File, report.List(e.Keys, 6)})
	}
	report.Table(os.Stdout, []string{"Repo", "File", "Keys"}, rows)
	fmt.Printf("%s would be backed up (dry run)\n", report.Plural(len(entries), "file", "files"))
}
