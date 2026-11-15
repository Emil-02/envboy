package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Emil-02/envboy/internal/discover"
	"github.com/Emil-02/envboy/internal/report"
)

func newScanCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scan [dir...]",
		Short: "Find env files in every repo and flag the ones git could leak",
		Example: `  envboy scan ~/Proyek
  envboy scan --json | jq '.[] | select(.env_files[].tracked)'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			repos, err := discover.Scan(roots(args), scanOptions())
			if err != nil {
				return err
			}
			if gf.jsonOut {
				return report.JSON(os.Stdout, withEnv(repos))
			}
			printScan(repos)
			if countRisk(repos, discover.RiskTracked) > 0 {
				return exitCode(2)
			}
			return nil
		},
	}
}

func withEnv(repos []discover.Repo) []discover.Repo {
	out := []discover.Repo{}
	for _, r := range repos {
		if len(r.EnvFiles) > 0 {
			out = append(out, r)
		}
	}
	return out
}

func countRisk(repos []discover.Repo, risk discover.Risk) int {
	n := 0
	for _, r := range repos {
		for _, f := range r.EnvFiles {
			if r.Risk(f) == risk {
				n++
			}
		}
	}
	return n
}

func fileLabel(r discover.Repo, f discover.File) string {
	switch r.Risk(f) {
	case discover.RiskTracked:
		return report.Bad.Render(f.Rel + " ✗ tracked")
	case discover.RiskUnignored:
		return report.Warn.Render(f.Rel + " ⚠ not ignored")
	}
	return f.Rel
}

func printScan(repos []discover.Repo) {
	var rows [][]string
	files, templateOnly := 0, 0
	for _, r := range repos {
		if len(r.EnvFiles) == 0 {
			templateOnly++
			continue
		}
		var labels []string
		for _, f := range r.EnvFiles {
			labels = append(labels, fileLabel(r, f))
			files++
		}
		remote := r.Remote
		if remote == "" {
			remote = report.Faint.Render("—")
			if !r.IsGit {
				remote = report.Faint.Render("not a git repo")
			}
		}
		rows = append(rows, []string{r.Name(), remote, strings.Join(labels, "\n")})
	}
	if len(rows) == 0 {
		fmt.Println(report.Faint.Render("No env files found."))
		return
	}
	report.Table(os.Stdout, []string{"Repo", "Remote", "Env files"}, rows)

	tracked, unignored := countRisk(repos, discover.RiskTracked), countRisk(repos, discover.RiskUnignored)
	fmt.Printf("%s in %s", report.Plural(files, "env file", "env files"), report.Plural(len(rows), "repo", "repos"))
	if templateOnly > 0 {
		fmt.Print(report.Faint.Render(fmt.Sprintf(" (+%d with only a template)", templateOnly)))
	}
	fmt.Println()
	if tracked > 0 {
		fmt.Println(report.Bad.Render(fmt.Sprintf("✗ %d committed to git.", tracked)),
			"Rotate those secrets, then run", report.Bold.Render("envboy guard fix --untrack"))
	}
	if unignored > 0 {
		fmt.Println(report.Warn.Render(fmt.Sprintf("⚠ %d not covered by .gitignore.", unignored)),
			"Run", report.Bold.Render("envboy guard fix"))
	}
	if tracked == 0 && unignored == 0 {
		fmt.Println(report.OK.Render("✓ Every env file is ignored by git."))
	}
}
