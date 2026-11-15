package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Emil-02/envboy/internal/check"
	"github.com/Emil-02/envboy/internal/discover"
	"github.com/Emil-02/envboy/internal/report"
)

func newCheckCmd() *cobra.Command {
	var strict bool
	c := &cobra.Command{
		Use:   "check [dir...]",
		Short: "Compare env files with .env.example: missing, extra and empty keys",
		Long: `For every env file that has a template next to it (.env.example, .env.sample,
.env.template, .env.dist, or <name>.example), reports keys the template has but the
env file lacks, keys only in the env file, and keys with empty values.

Exits with 1 when any key is missing. With --strict, extra keys, empty values and
templates without an env file also fail. Handy in CI and in a predev script.`,
		Example: `  envboy check
  envboy check ~/Proyek --strict
  envboy check --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			repos, err := discover.Scan(roots(args), scanOptions())
			if err != nil {
				return err
			}
			findings := check.All(repos)
			failed := false
			for _, f := range findings {
				if f.Failed(strict) {
					failed = true
				}
			}
			if gf.jsonOut {
				if findings == nil {
					findings = []check.Finding{}
				}
				if err := report.JSON(os.Stdout, findings); err != nil {
					return err
				}
			} else {
				printFindings(findings)
			}
			if failed {
				return exitCode(1)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&strict, "strict", false, "also fail on extra keys, empty values and missing env files")
	return c
}

func printFindings(findings []check.Finding) {
	if len(findings) == 0 {
		fmt.Println(report.Faint.Render("No env file has a template to compare with."))
		return
	}
	var rows [][]string
	ok := 0
	for _, f := range findings {
		if f.OK() {
			ok++
			continue
		}
		rows = append(rows, []string{f.Repo, findingFile(f), findingProblems(f)})
	}
	report.Table(os.Stdout, []string{"Repo", "File", "Problems"}, rows)
	if len(rows) == 0 {
		fmt.Println(report.OK.Render(fmt.Sprintf("✓ All %s match their templates.", report.Plural(ok, "env file", "env files"))))
		return
	}
	fmt.Printf("%d ok · %d with problems\n", ok, len(rows))
}

func findingFile(f check.Finding) string {
	if f.NoEnvFile {
		return report.Faint.Render("(none) ← " + f.Template)
	}
	return f.File + report.Faint.Render(" ↔ "+f.Template)
}

func findingProblems(f check.Finding) string {
	var lines []string
	if f.Error != "" {
		lines = append(lines, report.Bad.Render("error: "+f.Error))
	}
	if f.NoEnvFile {
		lines = append(lines, report.Warn.Render("template has no env file"))
	}
	if len(f.Missing) > 0 {
		lines = append(lines, report.Bad.Render("missing: ")+report.List(f.Missing, 6))
	}
	if len(f.Empty) > 0 {
		lines = append(lines, report.Warn.Render("empty: ")+report.List(f.Empty, 6))
	}
	if len(f.Extra) > 0 {
		lines = append(lines, report.Info.Render("not in template: ")+report.List(f.Extra, 6))
	}
	return strings.Join(lines, "\n")
}
