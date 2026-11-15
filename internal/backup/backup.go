// Package backup turns discovered env files into vault entries.
package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/Emil-02/envboy/internal/discover"
	"github.com/Emil-02/envboy/internal/dotenv"
	"github.com/Emil-02/envboy/internal/vault"
)

// Collect reads every env file in repos. Templates are not backed up: they are committed.
func Collect(repos []discover.Repo) (entries []vault.Entry, scanned map[string]bool, errs []error) {
	scanned = map[string]bool{}
	for _, r := range repos {
		scanned[r.Key] = true
		for _, f := range r.EnvFiles {
			e, err := EntryFor(r, f)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s/%s: %w", r.Name(), f.Rel, err))
				continue
			}
			entries = append(entries, e)
		}
	}
	return entries, scanned, errs
}

// EntryFor reads one file into an entry.
func EntryFor(r discover.Repo, f discover.File) (vault.Entry, error) {
	data, err := os.ReadFile(f.Path)
	if err != nil {
		return vault.Entry{}, err
	}
	st, err := os.Stat(f.Path)
	if err != nil {
		return vault.Entry{}, err
	}
	return vault.Entry{
		Repo:    r.Key,
		Remote:  r.Remote,
		Path:    r.RelPath,
		File:    f.Rel,
		Mode:    uint32(st.Mode().Perm()),
		ModTime: st.ModTime().UTC().Truncate(1e9),
		SHA256:  Hash(data),
		Keys:    dotenv.Names(data),
		Content: data,
	}, nil
}

// Hash is the hex SHA-256 of data.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
