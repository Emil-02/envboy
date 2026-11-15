// Package tui is envboy's interactive dashboard.
package tui

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Emil-02/envboy/internal/backup"
	"github.com/Emil-02/envboy/internal/check"
	"github.com/Emil-02/envboy/internal/discover"
	"github.com/Emil-02/envboy/internal/dotenv"
	"github.com/Emil-02/envboy/internal/guard"
	"github.com/Emil-02/envboy/internal/keys"
	"github.com/Emil-02/envboy/internal/restore"
	"github.com/Emil-02/envboy/internal/vault"
)

// Options configures the dashboard.
type Options struct {
	Roots           []string
	Scan            discover.Options
	VaultPath       string
	TemplateMarkers []string
	// Keys returns a fresh key source (identities and recipients from config).
	Keys func() *keys.Source
	// Bin is the envboy binary path written into git hooks.
	Bin     string
	Version string
}

// Run starts the dashboard.
func Run(o Options) error {
	_, err := tea.NewProgram(newModel(o), tea.WithAltScreen()).Run()
	return err
}

var errNeedPass = errors.New("passphrase needed")

type vaultState int

const (
	vaultMissing vaultState = iota
	vaultLocked
	vaultOpen
)

type inputPurpose int

const (
	inputNone inputPurpose = iota
	inputUnlock
	inputNewPass
	inputNewPassConfirm
	inputFilter
)

type action int

const (
	actNone action = iota
	actBackup
	actRestore
	actFix
	actHook
)

type model struct {
	opt Options

	repos    []discover.Repo // every repo found, including ones without env files
	keyNames map[string][]string
	hooks    map[string]bool
	findings map[string][]check.Finding
	items    map[string][]restore.Item
	notInVlt map[string]map[string]bool // repo dir -> file -> true

	vstate vaultState
	vlt    *vault.Vault
	enc    *keys.Encryption

	cursor, offset int
	width, height  int
	filter         string

	input    textinput.Model
	purpose  inputPurpose
	newPass  string
	pending  action
	confirm  action
	busy     bool
	spin     spinner.Model
	msg      string
	msgStyle lipgloss.Style
	help     bool
	// needsPass is set once we know the vault is passphrase-encrypted.
	needsPass bool
}

func newModel(o Options) model {
	ti := textinput.New()
	ti.CharLimit = 256
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = accent
	m := model{opt: o, input: ti, spin: sp, findings: map[string][]check.Finding{}}
	m.vstate = vaultMissing
	if st, err := os.Stat(o.VaultPath); err == nil && st.Mode().IsRegular() {
		m.vstate = vaultLocked
	}
	m.busy = true
	return m
}

// ---- messages ----

type scanMsg struct {
	repos    []discover.Repo
	keyNames map[string][]string
	hooks    map[string]bool
	err      error
}

type vaultMsg struct {
	v   *vault.Vault
	enc *keys.Encryption
	err error
}

type doneMsg struct {
	text string
	err  error
	v    *vault.Vault
	enc  *keys.Encryption
}

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spin.Tick, m.scanCmd()}
	if m.vstate == vaultLocked {
		cmds = append(cmds, m.unlockCmd("")) // works without prompting when a key file is configured
	}
	return tea.Batch(cmds...)
}

func (m model) scanCmd() tea.Cmd {
	opt := m.opt.Scan
	opt.AllRepos = true
	roots := m.opt.Roots
	return func() tea.Msg {
		repos, err := discover.Scan(roots, opt)
		if err != nil {
			return scanMsg{err: err}
		}
		msg := scanMsg{repos: repos, keyNames: map[string][]string{}, hooks: map[string]bool{}}
		for _, r := range repos {
			if len(r.EnvFiles) == 0 && len(r.Templates) == 0 {
				continue
			}
			for _, f := range r.EnvFiles {
				if data, err := os.ReadFile(f.Path); err == nil {
					msg.keyNames[f.Path] = dotenv.Names(data)
				}
			}
			if r.IsGit {
				msg.hooks[r.Dir] = guard.Installed(r.Dir)
			}
		}
		return msg
	}
}

func (m model) source(pass string) *keys.Source {
	s := m.opt.Keys()
	s.Passphrase = pass
	s.Prompt = func(string, bool) (string, error) { return "", errNeedPass }
	return s
}

func (m model) unlockCmd(pass string) tea.Cmd {
	src := m.source(pass)
	path := m.opt.VaultPath
	return func() tea.Msg {
		u, err := src.Unlocker()
		if err != nil {
			return vaultMsg{err: err}
		}
		v, err := vault.Read(path, u.Identities())
		if err != nil {
			return vaultMsg{err: err}
		}
		enc, err := u.Encryption()
		return vaultMsg{v: v, enc: enc, err: err}
	}
}

// ---- update ----

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case scanMsg:
		m.busy = false
		if msg.err != nil {
			m.setErr(msg.err)
			return m, nil
		}
		m.keyNames, m.hooks = msg.keyNames, msg.hooks
		m.setRepos(msg.repos)
		return m, nil
	case vaultMsg:
		m.busy = false
		switch {
		case errors.Is(msg.err, errNeedPass):
			m.needsPass = true
			if m.pending == actNone && m.purpose == inputNone {
				m.setInfo("Vault is locked: press u to unlock")
				return m, nil
			}
			return m, m.ask(inputUnlock, "Vault passphrase")
		case errors.Is(msg.err, keys.ErrWrongPassphrase):
			m.setErr(msg.err)
			return m, m.ask(inputUnlock, "Vault passphrase (try again)")
		case msg.err != nil:
			m.pending = actNone
			m.setErr(msg.err)
			return m, nil
		}
		m.vlt, m.enc, m.vstate = msg.v, msg.enc, vaultOpen
		m.recompute()
		m.setOK("Vault unlocked: " + plural(len(msg.v.Entries), "file"))
		if p := m.pending; p != actNone {
			m.pending = actNone
			return m.start(p)
		}
		return m, nil
	case doneMsg:
		m.busy = false
		if msg.err != nil {
			m.setErr(msg.err)
			return m, nil
		}
		if msg.v != nil {
			m.vlt, m.vstate = msg.v, vaultOpen
		}
		if msg.enc != nil {
			m.enc = msg.enc
		}
		m.setOK(msg.text)
		m.busy = true
		return m, tea.Batch(m.spin.Tick, m.scanCmd())
	case tea.KeyMsg:
		if m.purpose != inputNone {
			return m.updateInput(msg)
		}
		if m.confirm != actNone {
			return m.updateConfirm(msg)
		}
		return m.updateKeys(msg)
	}
	return m, nil
}

func (m model) updateKeys(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.help {
		m.help = false
		return m, nil
	}
	list := m.visible()
	switch k.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.help = true
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(list)-1 {
			m.cursor++
		}
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = max(0, len(list)-1)
	case "pgdown":
		m.cursor = min(max(0, len(list)-1), m.cursor+m.listHeight())
	case "pgup":
		m.cursor = max(0, m.cursor-m.listHeight())
	case "/":
		return m, m.ask(inputFilter, "Filter")
	case "esc":
		m.filter, m.cursor = "", 0
	case "r":
		if !m.busy {
			m.busy = true
			m.setInfo("Rescanning…")
			return m, tea.Batch(m.spin.Tick, m.scanCmd())
		}
	case "u":
		if m.vstate == vaultLocked && !m.busy {
			if m.needsPass {
				return m, m.ask(inputUnlock, "Vault passphrase")
			}
			m.busy = true
			return m, tea.Batch(m.spin.Tick, m.unlockCmd(""))
		}
	case "b":
		return m.request(actBackup)
	case "R":
		return m.request(actRestore)
	case "f":
		return m.request(actFix)
	case "h":
		return m.request(actHook)
	}
	return m, nil
}

// request asks for confirmation of an action that writes files.
func (m model) request(a action) (tea.Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	sel := m.selected()
	switch a {
	case actRestore:
		if sel == nil {
			return m, nil
		}
		if m.vstate == vaultMissing {
			m.setErr(errors.New("no vault yet: press b to back up first"))
			return m, nil
		}
		if m.vstate == vaultOpen && countAction(m.items[sel.Dir], restore.Create) == 0 {
			m.setInfo("Nothing to restore here (existing files are never overwritten from the dashboard)")
			return m, nil
		}
	case actFix:
		if sel == nil || !sel.IsGit || riskCount(*sel) == 0 {
			m.setInfo("Nothing to fix: every env file here is ignored")
			return m, nil
		}
	case actHook:
		if sel == nil || !sel.IsGit {
			m.setInfo("Not a git repository")
			return m, nil
		}
	}
	m.confirm = a
	return m, nil
}

func (m model) updateConfirm(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	a := m.confirm
	m.confirm = actNone
	switch k.String() {
	case "y", "Y", "enter":
		return m.start(a)
	}
	m.setInfo("Cancelled")
	return m, nil
}

// start runs an action, unlocking or creating the vault first when needed.
func (m model) start(a action) (tea.Model, tea.Cmd) {
	needsVault := a == actBackup || a == actRestore
	if needsVault && m.vstate == vaultLocked {
		m.pending = a
		if m.needsPass {
			return m, m.ask(inputUnlock, "Vault passphrase")
		}
		m.busy = true
		return m, tea.Batch(m.spin.Tick, m.unlockCmd(""))
	}
	if a == actRestore {
		sel := m.selected()
		if sel == nil || countAction(m.items[sel.Dir], restore.Create) == 0 {
			m.setInfo("Nothing to restore here (existing files are never overwritten from the dashboard)")
			return m, nil
		}
	}
	if a == actBackup && m.enc == nil {
		enc, err := m.source("").Encryption()
		switch {
		case errors.Is(err, errNeedPass):
			m.pending = a
			return m, m.ask(inputNewPass, "New vault passphrase")
		case err != nil:
			m.setErr(err)
			return m, nil
		}
		m.enc = enc
	}
	m.busy = true
	return m, tea.Batch(m.spin.Tick, m.actionCmd(a))
}

func (m model) actionCmd(a action) tea.Cmd {
	sel := m.selected()
	switch a {
	case actBackup:
		repos := append([]discover.Repo(nil), m.repos...)
		v, enc, path, version := m.vlt, m.enc, m.opt.VaultPath, m.opt.Version
		return func() tea.Msg {
			entries, scanned, errs := backup.Collect(repos)
			nv := vault.New("envboy " + version)
			if v != nil {
				cp := *v
				cp.Entries = append([]vault.Entry(nil), v.Entries...)
				nv = &cp
			}
			nv.Replace(entries, scanned, true)
			if err := vault.Write(path, nv, enc.Recipients, true); err != nil {
				return doneMsg{err: err}
			}
			text := fmt.Sprintf("Backed up %s to %s (%s)", plural(len(entries), "file"), path, enc.Mode)
			if len(errs) > 0 {
				text += fmt.Sprintf(", %d skipped", len(errs))
			}
			return doneMsg{text: text, v: nv, enc: enc}
		}
	case actRestore:
		if sel == nil {
			return nil
		}
		items := m.items[sel.Dir]
		return func() tea.Msg {
			res := restore.Apply(restore.Plan{Items: items}, restore.Options{})
			if len(res.Errors) > 0 {
				return doneMsg{err: res.Errors[0]}
			}
			return doneMsg{text: fmt.Sprintf("Restored %s into %s", plural(len(res.Written), "file"), sel.Name())}
		}
	case actFix:
		if sel == nil {
			return nil
		}
		r, markers := *sel, m.opt.TemplateMarkers
		return func() tea.Msg {
			if _, err := guard.Fix(r, markers, false); err != nil {
				return doneMsg{err: err}
			}
			return doneMsg{text: "Updated .gitignore in " + r.Name()}
		}
	case actHook:
		if sel == nil {
			return nil
		}
		r, bin := *sel, m.opt.Bin
		return func() tea.Msg {
			res, err := guard.Install(r.Dir, bin)
			if err != nil {
				return doneMsg{err: err}
			}
			text := "Pre-commit hook installed in " + r.Name()
			if res.Chained {
				text += " (existing hook kept)"
			}
			return doneMsg{text: text}
		}
	}
	return nil
}

func (m *model) ask(p inputPurpose, prompt string) tea.Cmd {
	m.purpose = p
	m.input.Reset()
	m.input.Prompt = prompt + ": "
	if p == inputFilter {
		m.input.EchoMode = textinput.EchoNormal
		m.input.SetValue(m.filter)
	} else {
		m.input.EchoMode = textinput.EchoPassword
		m.input.EchoCharacter = '•'
	}
	return m.input.Focus()
}

func (m model) updateInput(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		if m.purpose == inputFilter {
			m.filter = ""
		}
		m.purpose, m.pending, m.newPass = inputNone, actNone, ""
		m.input.Blur()
		m.setInfo("Cancelled")
		return m, nil
	case "enter":
		val := m.input.Value()
		p := m.purpose
		m.purpose = inputNone
		m.input.Blur()
		m.input.Reset()
		switch p {
		case inputFilter:
			m.filter, m.cursor, m.offset = strings.TrimSpace(val), 0, 0
			return m, nil
		case inputUnlock:
			m.busy = true
			return m, tea.Batch(m.spin.Tick, m.unlockCmd(val))
		case inputNewPass:
			if val == "" {
				m.setErr(errors.New("empty passphrase"))
				return m, m.ask(inputNewPass, "New vault passphrase")
			}
			m.newPass = val
			return m, m.ask(inputNewPassConfirm, "Repeat passphrase")
		case inputNewPassConfirm:
			if val != m.newPass {
				m.newPass = ""
				m.setErr(errors.New("passphrases do not match"))
				return m, m.ask(inputNewPass, "New vault passphrase")
			}
			enc, err := keys.PassphraseEncryption(val)
			m.newPass = ""
			if err != nil {
				m.setErr(err)
				return m, nil
			}
			m.enc = enc
			a := m.pending
			m.pending = actNone
			return m.start(a)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	if m.purpose == inputFilter {
		m.filter, m.cursor, m.offset = strings.TrimSpace(m.input.Value()), 0, 0
	}
	return m, cmd
}

// ---- derived state ----

func (m *model) setRepos(all []discover.Repo) {
	var keep []discover.Repo
	m.findings = map[string][]check.Finding{}
	for _, r := range all {
		keep = append(keep, r)
		if f := check.Repo(r); len(f) > 0 {
			m.findings[r.Dir] = f
		}
	}
	m.repos = keep
	m.recompute()
	if m.cursor >= len(m.visible()) {
		m.cursor = max(0, len(m.visible())-1)
	}
}

// recompute rebuilds per-repo vault status.
func (m *model) recompute() {
	m.items = map[string][]restore.Item{}
	m.notInVlt = map[string]map[string]bool{}
	if m.vlt == nil {
		return
	}
	st := restore.BuildStatus(m.vlt, m.repos, m.opt.Roots)
	for _, it := range st.Plan.Items {
		if it.RepoDir != "" {
			m.items[it.RepoDir] = append(m.items[it.RepoDir], it)
		}
	}
	for _, f := range st.NotBackedUp {
		if m.notInVlt[f.Dir] == nil {
			m.notInVlt[f.Dir] = map[string]bool{}
		}
		m.notInVlt[f.Dir][f.File] = true
	}
}

// visible lists repos worth showing: those with env files, templates, or vault entries.
func (m model) visible() []discover.Repo {
	var out []discover.Repo
	q := strings.ToLower(m.filter)
	for _, r := range m.repos {
		if len(r.EnvFiles) == 0 && len(r.Templates) == 0 && len(m.items[r.Dir]) == 0 {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(r.Name()+" "+r.Remote), q) {
			continue
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

func (m model) selected() *discover.Repo {
	list := m.visible()
	if m.cursor < 0 || m.cursor >= len(list) {
		return nil
	}
	r := list[m.cursor]
	return &r
}

func (m *model) setErr(err error) { m.msg, m.msgStyle = "✗ "+err.Error(), bad }
func (m *model) setOK(s string)   { m.msg, m.msgStyle = "✓ "+s, good }
func (m *model) setInfo(s string) { m.msg, m.msgStyle = s, faint }

func countAction(items []restore.Item, a restore.Action) int {
	n := 0
	for _, it := range items {
		if it.Action == a {
			n++
		}
	}
	return n
}

func riskCount(r discover.Repo) int {
	n := 0
	for _, f := range r.EnvFiles {
		if r.Risk(f) != discover.RiskNone {
			n++
		}
	}
	return n
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
