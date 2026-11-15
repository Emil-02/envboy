// Package vault defines the encrypted backup format and reads/writes it with age.
package vault

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"filippo.io/age"
	"filippo.io/age/armor"
)

// FormatVersion is the payload schema version.
const FormatVersion = 1

// Vault is the decrypted payload.
type Vault struct {
	Version   int       `json:"version"`
	Tool      string    `json:"tool"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Hostname  string    `json:"hostname"`
	Entries   []Entry   `json:"entries"`
}

// Entry is one backed-up file.
type Entry struct {
	// Repo is the repo key (normalized remote, "path:<rel>", or either with "@<rel>").
	Repo string `json:"repo"`
	// Remote is the normalized remote URL, empty for repos without one.
	Remote string `json:"remote,omitempty"`
	// Path is the repo directory relative to the scan root (slash-separated).
	Path string `json:"path"`
	// File is the file path relative to the repo directory (slash-separated).
	File    string    `json:"file"`
	Mode    uint32    `json:"mode"`
	ModTime time.Time `json:"mod_time"`
	SHA256  string    `json:"sha256"`
	Keys    []string  `json:"keys"`
	Content []byte    `json:"content"`
}

// New returns an empty vault.
func New(tool string) *Vault {
	host, _ := os.Hostname()
	now := time.Now().UTC().Truncate(time.Second)
	return &Vault{Version: FormatVersion, Tool: tool, CreatedAt: now, UpdatedAt: now, Hostname: host}
}

// Sort orders entries by repo then file.
func (v *Vault) Sort() {
	sort.Slice(v.Entries, func(i, j int) bool {
		a, b := v.Entries[i], v.Entries[j]
		if a.Repo != b.Repo {
			return a.Repo < b.Repo
		}
		return a.File < b.File
	})
}

// Replace swaps in fresh entries. When merge is true, entries of repos not in
// scanned are kept (they live on another machine or were not scanned this
// time); entries of scanned repos are always fully replaced, so deleted files drop out.
func (v *Vault) Replace(fresh []Entry, scanned map[string]bool, merge bool) {
	var kept []Entry
	if merge {
		for _, e := range v.Entries {
			if !scanned[e.Repo] {
				kept = append(kept, e)
			}
		}
	}
	v.Entries = append(kept, fresh...)
	v.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	if h, err := os.Hostname(); err == nil {
		v.Hostname = h
	}
	v.Sort()
}

// Repos returns the number of distinct repos.
func (v *Vault) Repos() int {
	seen := map[string]bool{}
	for _, e := range v.Entries {
		seen[e.Repo] = true
	}
	return len(seen)
}

// Encrypt writes v encrypted to recipients.
func Encrypt(w io.Writer, v *Vault, recipients []age.Recipient, armored bool) error {
	if len(recipients) == 0 {
		return errors.New("no recipients")
	}
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	out := w
	var aw io.WriteCloser
	if armored {
		aw = armor.NewWriter(w)
		out = aw
	}
	ew, err := age.Encrypt(out, recipients...)
	if err != nil {
		return err
	}
	if _, err := ew.Write(payload); err != nil {
		return err
	}
	if err := ew.Close(); err != nil {
		return err
	}
	if aw != nil {
		return aw.Close()
	}
	return nil
}

// Decrypt reads an armored or binary age file and decodes the payload.
func Decrypt(r io.Reader, identities []age.Identity) (*Vault, error) {
	br := bufio.NewReader(r)
	var in io.Reader = br
	if head, _ := br.Peek(len(armor.Header)); string(head) == armor.Header {
		in = armor.NewReader(br)
	}
	dr, err := age.Decrypt(in, identities...)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(dr)
	if err != nil {
		return nil, err
	}
	var v Vault
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("decode vault: %w", err)
	}
	if v.Version != FormatVersion {
		return nil, fmt.Errorf("unsupported vault version %d (this envboy reads version %d)", v.Version, FormatVersion)
	}
	return &v, nil
}

// Read decrypts the vault at path.
func Read(path string, identities []age.Identity) (*Vault, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return Decrypt(f, identities)
}

// Write encrypts v to path atomically with 0600 permissions.
func Write(path string, v *Vault, recipients []age.Recipient, armored bool) error {
	var buf bytes.Buffer
	if err := Encrypt(&buf, v, recipients, armored); err != nil {
		return err
	}
	return WriteFileAtomic(path, buf.Bytes(), 0o600)
}

// WriteFileAtomic writes data to a temp file in the same directory and renames it over path.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
