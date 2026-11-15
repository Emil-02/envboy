package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Emil-02/envboy/internal/config"
	"github.com/Emil-02/envboy/internal/discover"
	"github.com/Emil-02/envboy/internal/keys"
	"github.com/Emil-02/envboy/internal/report"
)

// Version is set by main from ldflags.
var Version = "dev"

type globalFlags struct {
	configPath string
	jsonOut    bool
	noColor    bool
	vault      string
	identities []string
}

var (
	gf  globalFlags
	cfg config.Config
)

// exitCode lets a command fail with a specific code without printing an error.
type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

// NewRoot builds the command tree.
func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "envboy",
		Short: "Back up, restore, check and guard .env files across all your repos",
		Long: `envboy finds every .env file under your project folders, backs them up into one
age-encrypted vault, restores them on a new machine by matching git remotes,
checks them against .env.example, and keeps them out of git.

Run without arguments in a terminal to open the dashboard.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if gf.noColor || os.Getenv("NO_COLOR") != "" {
				report.DisableColor()
			}
			var err error
			cfg, err = config.Load(gf.configPath)
			if err != nil {
				return err
			}
			if gf.vault != "" {
				cfg.Vault = config.Expand(gf.vault)
			}
			if len(gf.identities) > 0 {
				cfg.Identities = nil
				for _, id := range gf.identities {
					cfg.Identities = append(cfg.Identities, config.Expand(id))
				}
			}
			return nil
		},
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !term.IsTerminal(int(os.Stdout.Fd())) || !term.IsTerminal(int(os.Stdin.Fd())) {
				return cmd.Help()
			}
			return runTUI(args)
		},
	}
	pf := root.PersistentFlags()
	pf.StringVar(&gf.configPath, "config", "", "config file (default "+config.Path()+")")
	pf.BoolVar(&gf.jsonOut, "json", false, "print JSON instead of tables")
	pf.BoolVar(&gf.noColor, "no-color", false, "disable colors (also honors NO_COLOR)")
	pf.StringVar(&gf.vault, "vault", "", "vault file (default ~/envboy.vault)")
	pf.StringArrayVarP(&gf.identities, "identity", "i", nil, "age identity file for decryption (repeatable)")

	root.AddCommand(newScanCmd(), newBackupCmd(), newRestoreCmd(), newStatusCmd(),
		newCheckCmd(), newGuardCmd(), newKeygenCmd(), newTUICmd())
	root.SetVersionTemplate("envboy {{.Version}}\n")
	return root
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	root := NewRoot()
	if err := root.Execute(); err != nil {
		var code exitCode
		if errors.As(err, &code) {
			return int(code)
		}
		fmt.Fprintln(os.Stderr, report.Bad.Render("error:"), err)
		return 1
	}
	return 0
}

// roots resolves scan roots: arguments, then config, then the working directory.
func roots(args []string) []string {
	if len(args) > 0 {
		return args
	}
	if len(cfg.Roots) > 0 {
		return cfg.Roots
	}
	wd, err := os.Getwd()
	if err != nil {
		return []string{"."}
	}
	return []string{wd}
}

func scanOptions() discover.Options { return discover.FromConfig(cfg) }

func keySource() *keys.Source {
	return &keys.Source{IdentityFiles: cfg.Identities, Recipients: cfg.Recipients, Prompt: promptSecret}
}

func vaultExists() bool {
	st, err := os.Stat(cfg.Vault)
	return err == nil && st.Mode().IsRegular()
}

func shortPath(p string) string {
	home, err := os.UserHomeDir()
	if err == nil {
		if rel, err := filepath.Rel(home, p); err == nil && filepath.IsLocal(rel) {
			return filepath.Join("~", rel)
		}
	}
	return p
}
