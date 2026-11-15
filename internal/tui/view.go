package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Emil-02/envboy/internal/discover"
	"github.com/Emil-02/envboy/internal/restore"
)

var (
	pink   = lipgloss.Color("212")
	accent = lipgloss.NewStyle().Foreground(lipgloss.Color("141"))
	title  = lipgloss.NewStyle().Bold(true).Foreground(pink)
	faint  = lipgloss.NewStyle().Faint(true)
	bold   = lipgloss.NewStyle().Bold(true)
	good   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warn   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	bad    = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	info   = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	cursor = lipgloss.NewStyle().Background(lipgloss.Color("236")).Bold(true)
	pane   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1)
)

func (m model) listHeight() int { return max(3, m.height-7) }

func (m model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.help {
		return m.helpView()
	}
	header := m.headerView()
	footer := m.footerView()
	bodyH := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	listW := min(52, max(28, m.width*2/5))
	detailW := m.width - listW

	list := pane.Width(listW - 2).Height(bodyH - 2).Render(m.listView(listW-4, bodyH-2))
	detail := pane.Width(detailW - 2).Height(bodyH - 2).Render(m.detailView(detailW-4, bodyH-2))
	return lipgloss.JoinVertical(lipgloss.Left, header, lipgloss.JoinHorizontal(lipgloss.Top, list, detail), footer)
}

func (m model) headerView() string {
	var vs string
	switch m.vstate {
	case vaultMissing:
		vs = faint.Render("no vault yet")
	case vaultLocked:
		vs = warn.Render("🔒 locked")
	case vaultOpen:
		vs = good.Render("🔓 " + plural(len(m.vlt.Entries), "file"))
	}
	roots := make([]string, len(m.opt.Roots))
	for i, r := range m.opt.Roots {
		roots[i] = short(r)
	}
	left := title.Render(" envboy ") + faint.Render(strings.Join(roots, ", "))
	right := faint.Render(short(m.opt.VaultPath)+" ") + vs + " "
	gap := max(1, m.width-lipgloss.Width(left)-lipgloss.Width(right))
	return left + strings.Repeat(" ", gap) + right
}

func (m model) footerView() string {
	var line string
	switch {
	case m.purpose != inputNone:
		line = " " + m.input.View()
	case m.confirm != actNone:
		line = " " + warn.Render(m.confirmText()) + faint.Render("  [y/N]")
	case m.busy:
		line = " " + m.spin.View() + " " + faint.Render("working…")
	case m.msg != "":
		line = " " + m.msgStyle.Render(truncate(m.msg, m.width-2))
	}
	keys := faint.Render(" ↑↓ move  / filter  b backup  R restore  f fix .gitignore  h hook  u unlock  r rescan  ? help  q quit")
	return line + "\n" + truncate(keys, m.width)
}

func (m model) confirmText() string {
	sel := m.selected()
	name := ""
	if sel != nil {
		name = sel.Name()
	}
	switch m.confirm {
	case actBackup:
		return fmt.Sprintf("Back up every env file shown to %s?", short(m.opt.VaultPath))
	case actRestore:
		if m.vstate == vaultOpen {
			return fmt.Sprintf("Restore %s into %s?", plural(countAction(m.items[sel.Dir], restore.Create), "missing file"), name)
		}
		return "Unlock the vault and restore missing files into " + name + "?"
	case actFix:
		return "Add env ignore rules to " + name + "/.gitignore?"
	case actHook:
		return "Install the envboy pre-commit hook in " + name + "?"
	}
	return ""
}

func (m model) listView(w, h int) string {
	list := m.visible()
	if len(list) == 0 {
		if m.busy {
			return faint.Render("Scanning…")
		}
		if m.filter != "" {
			return faint.Render("No repo matches “" + m.filter + "”")
		}
		return faint.Render("No env files found under these folders.")
	}
	// keep the cursor in view
	off := m.offset
	if m.cursor < off {
		off = m.cursor
	}
	if m.cursor >= off+h {
		off = m.cursor - h + 1
	}
	var b strings.Builder
	for i := off; i < len(list) && i < off+h; i++ {
		r := list[i]
		badges := m.badges(r)
		name := truncate(r.Name(), w-lipgloss.Width(badges)-1)
		row := name + strings.Repeat(" ", max(1, w-lipgloss.Width(name)-lipgloss.Width(badges))) + badges
		if i == m.cursor {
			row = cursor.Render(row)
		}
		b.WriteString(row)
		if i < len(list)-1 && i < off+h-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (m model) badges(r discover.Repo) string {
	var parts []string
	tracked, unignored := 0, 0
	for _, f := range r.EnvFiles {
		switch r.Risk(f) {
		case discover.RiskTracked:
			tracked++
		case discover.RiskUnignored:
			unignored++
		}
	}
	if tracked > 0 {
		parts = append(parts, bad.Render("✗"))
	} else if unignored > 0 {
		parts = append(parts, warn.Render("⚠"))
	}
	missing := 0
	for _, f := range m.findings[r.Dir] {
		missing += len(f.Missing)
	}
	if missing > 0 {
		parts = append(parts, bad.Render(fmt.Sprintf("!%d", missing)))
	}
	if m.vstate == vaultOpen {
		items := m.items[r.Dir]
		switch {
		case countAction(items, restore.Create) > 0:
			parts = append(parts, info.Render(fmt.Sprintf("↓%d", countAction(items, restore.Create))))
		case countAction(items, restore.Conflict) > 0 || len(m.notInVlt[r.Dir]) > 0:
			parts = append(parts, accent.Render("●"))
		case len(r.EnvFiles) > 0:
			parts = append(parts, good.Render("✓"))
		}
	}
	parts = append(parts, faint.Render(fmt.Sprintf("%d", len(r.EnvFiles))))
	return strings.Join(parts, " ")
}

func (m model) detailView(w, h int) string {
	r := m.selected()
	if r == nil {
		return ""
	}
	var b strings.Builder
	line := func(s string) { b.WriteString(truncate(s, w) + "\n") }

	line(title.Render(r.Name()))
	line(faint.Render(short(r.Dir)))
	switch {
	case r.Remote != "":
		line(accent.Render(r.Remote))
	case r.IsGit:
		line(faint.Render("git repo without a remote (matched by path)"))
	default:
		line(faint.Render("not a git repo (matched by path)"))
	}
	if r.IsGit {
		if m.hooks[r.Dir] {
			line(good.Render("✓ pre-commit guard installed"))
		} else {
			line(faint.Render("no pre-commit guard (h to install)"))
		}
	}
	line("")

	items := map[string]restore.Item{}
	for _, it := range m.items[r.Dir] {
		items[it.File] = it
	}
	if len(r.EnvFiles) > 0 {
		line(bold.Render("Env files"))
	}
	for _, f := range r.EnvFiles {
		status := ""
		switch r.Risk(f) {
		case discover.RiskTracked:
			status += bad.Render(" ✗ committed")
		case discover.RiskUnignored:
			status += warn.Render(" ⚠ not ignored")
		}
		if m.vstate == vaultOpen {
			if it, ok := items[f.Rel]; ok {
				switch it.Action {
				case restore.Identical:
					status += good.Render(" ✓ backed up")
				case restore.Conflict:
					status += accent.Render(" ● changed since backup")
				}
			} else if m.notInVlt[r.Dir][f.Rel] {
				status += accent.Render(" ● not backed up")
			}
		}
		names := m.keyNames[f.Path]
		line(" " + f.Rel + status + faint.Render("  "+plural(len(names), "key")))
		for _, l := range wrapList(names, w-3) {
			line("   " + faint.Render(l))
		}
		if it, ok := items[f.Rel]; ok && it.Action == restore.Conflict {
			if len(it.VaultOnly) > 0 {
				line("   " + info.Render("only in vault: ") + strings.Join(it.VaultOnly, ", "))
			}
			if len(it.LocalOnly) > 0 {
				line("   " + info.Render("only on disk: ") + strings.Join(it.LocalOnly, ", "))
			}
		}
	}
	var restorable []string
	for _, it := range m.items[r.Dir] {
		if it.Action == restore.Create {
			restorable = append(restorable, it.File)
		}
	}
	if len(restorable) > 0 {
		line("")
		line(bold.Render("In the vault, missing here") + faint.Render("  (R to restore)"))
		for _, f := range restorable {
			line(" " + info.Render("↓ "+f))
		}
	}
	if fs := m.findings[r.Dir]; len(fs) > 0 {
		line("")
		line(bold.Render("Check against templates"))
		for _, f := range fs {
			switch {
			case f.NoEnvFile:
				line(" " + warn.Render(f.Template+": no env file"))
			case f.OK():
				line(" " + good.Render("✓ "+f.File+" matches "+f.Template))
			default:
				line(" " + f.File + faint.Render(" ↔ "+f.Template))
				if len(f.Missing) > 0 {
					line("   " + bad.Render("missing: ") + strings.Join(f.Missing, ", "))
				}
				if len(f.Empty) > 0 {
					line("   " + warn.Render("empty: ") + strings.Join(f.Empty, ", "))
				}
				if len(f.Extra) > 0 {
					line("   " + info.Render("not in template: ") + strings.Join(f.Extra, ", "))
				}
			}
		}
	}
	if len(r.Skipped) > 0 {
		line("")
		line(faint.Render("skipped (too large or unreadable): " + strings.Join(r.Skipped, ", ")))
	}
	out := strings.TrimRight(b.String(), "\n")
	if lines := strings.Split(out, "\n"); len(lines) > h {
		out = strings.Join(lines[:h-1], "\n") + "\n" + faint.Render("…")
	}
	return out
}

func (m model) helpView() string {
	rows := [][2]string{
		{"↑/k ↓/j", "move"}, {"g / G", "first / last"}, {"/", "filter repos, esc clears"},
		{"b", "back up every env file shown into the vault (merges)"},
		{"R", "restore files that are in the vault but missing here (never overwrites)"},
		{"f", "add env ignore rules to the selected repo's .gitignore"},
		{"h", "install the pre-commit guard in the selected repo"},
		{"u", "unlock the vault to see backup status"},
		{"r", "rescan"}, {"q", "quit"},
	}
	var b strings.Builder
	b.WriteString(title.Render("envboy "+m.opt.Version) + "\n\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-10s %s\n", bold.Render(r[0]), r[1])
	}
	b.WriteString("\n" + bold.Render("Badges") + "\n")
	b.WriteString("  " + bad.Render("✗") + " env file committed   " + warn.Render("⚠") + " not in .gitignore   " +
		bad.Render("!3") + " keys missing vs template\n")
	b.WriteString("  " + good.Render("✓") + " backed up   " + accent.Render("●") + " changed / not backed up   " +
		info.Render("↓2") + " restorable from vault\n")
	b.WriteString("\n" + faint.Render("Values are never shown. Overwriting and pruning are CLI-only: envboy restore --help") + "\n")
	b.WriteString("\n" + faint.Render("press any key"))
	return pane.Width(m.width - 2).Render(b.String())
}

func wrapList(items []string, w int) []string {
	var lines []string
	cur := ""
	for _, it := range items {
		switch {
		case cur == "":
			cur = it
		case len(cur)+2+len(it) > w:
			lines = append(lines, cur)
			cur = it
		default:
			cur += ", " + it
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

func short(p string) string {
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, p); err == nil && filepath.IsLocal(rel) {
			return filepath.Join("~", rel)
		}
	}
	return p
}
