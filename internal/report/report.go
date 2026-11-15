// Package report renders terminal output: styles, tables and JSON.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/muesli/termenv"
)

var (
	Title  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	Faint  = lipgloss.NewStyle().Faint(true)
	Bold   = lipgloss.NewStyle().Bold(true)
	OK     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	Warn   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	Bad    = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	Info   = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	Accent = lipgloss.NewStyle().Foreground(lipgloss.Color("141"))
)

// DisableColor turns off ANSI styling everywhere.
func DisableColor() { lipgloss.SetColorProfile(termenv.Ascii) }

// Table renders rows with a header.
func Table(w io.Writer, headers []string, rows [][]string) {
	if len(rows) == 0 {
		return
	}
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(Faint).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, _ int) lipgloss.Style {
			s := lipgloss.NewStyle().Padding(0, 1)
			if row == table.HeaderRow {
				return s.Bold(true).Foreground(lipgloss.Color("212"))
			}
			return s
		})
	fmt.Fprintln(w, t.Render())
}

// JSON writes v as indented JSON.
func JSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// List joins names, truncating long lists.
func List(names []string, max int) string {
	if len(names) <= max || max <= 0 {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:max], ", ") + fmt.Sprintf(" +%d", len(names)-max)
}

// Plural returns "1 file" / "2 files".
func Plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// Stderr is where progress and hints go, so stdout stays machine-readable.
var Stderr io.Writer = os.Stderr
