package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Emil-02/envboy/internal/config"
	"github.com/Emil-02/envboy/internal/keys"
	"github.com/Emil-02/envboy/internal/report"
)

func newKeygenCmd() *cobra.Command {
	var out string
	c := &cobra.Command{
		Use:   "keygen",
		Short: "Create an age key so backups need no passphrase",
		Long: `Creates an age X25519 identity (default ` + config.DefaultIdentityPath() + `).
Once it exists, backup and restore use it automatically instead of a passphrase.

Copy the key file to your other machines (or a password manager): without it,
vaults encrypted to it cannot be opened.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if out == "" {
				out = config.DefaultIdentityPath()
			}
			out = config.Expand(out)
			if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
				return err
			}
			pub, err := keys.Generate(out)
			if err != nil {
				if os.IsExist(err) {
					return fmt.Errorf("%s already exists; refusing to overwrite a key", shortPath(out))
				}
				return err
			}
			fmt.Println(report.OK.Render("✓"), "identity written to", report.Bold.Render(shortPath(out)))
			fmt.Println("  public key:", report.Accent.Render(pub))
			fmt.Println(report.Faint.Render("  Back up this file. Share the public key with teammates who should be able to read your vaults."))
			return nil
		},
	}
	c.Flags().StringVarP(&out, "output", "o", "", "where to write the identity")
	return c
}
