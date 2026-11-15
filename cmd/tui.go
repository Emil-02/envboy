package cmd

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Emil-02/envboy/internal/keys"
	"github.com/Emil-02/envboy/internal/tui"
)

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui [dir...]",
		Short: "Open the interactive dashboard (same as running envboy with no command)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI(args)
		},
	}
}

func runTUI(args []string) error {
	bin, err := os.Executable()
	if err == nil {
		if resolved, err := filepath.EvalSymlinks(bin); err == nil {
			bin = resolved
		}
	} else {
		bin = "envboy"
	}
	rs := roots(args)
	for i, r := range rs {
		if abs, err := filepath.Abs(r); err == nil {
			rs[i] = abs
		}
	}
	return tui.Run(tui.Options{
		Roots:           rs,
		Scan:            scanOptions(),
		VaultPath:       cfg.Vault,
		TemplateMarkers: cfg.TemplateMarkers,
		Keys:            func() *keys.Source { return keySource() },
		Bin:             bin,
		Version:         Version,
	})
}
