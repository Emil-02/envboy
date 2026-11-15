// Package keys resolves how a vault is encrypted (passphrase or age keys)
// and lazily asks for a passphrase only when the vault needs one.
package keys

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"filippo.io/age"
)

// PassphraseEnv is read before prompting.
const PassphraseEnv = "ENVBOY_PASSPHRASE"

// Prompter asks the user for a secret. When confirm is true it should ask twice.
type Prompter func(prompt string, confirm bool) (string, error)

// ErrNoPassphrase is returned when a passphrase is needed but none is available.
var ErrNoPassphrase = errors.New("a passphrase is required: set " + PassphraseEnv + " or run in a terminal")

// ErrWrongPassphrase is returned when the vault passphrase does not match.
var ErrWrongPassphrase = errors.New("incorrect passphrase")

// Source collects every way the user told us about keys.
type Source struct {
	// ForcePassphrase ignores configured keys and encrypts with a passphrase.
	ForcePassphrase bool
	// Recipients are age public keys ("age1...").
	Recipients []string
	// RecipientFiles contain one recipient per line.
	RecipientFiles []string
	// IdentityFiles are age identity files (AGE-SECRET-KEY-1...).
	IdentityFiles []string
	// Passphrase overrides ENVBOY_PASSPHRASE and the prompt.
	Passphrase string
	Prompt     Prompter
}

// Mode describes how a vault is (or will be) encrypted.
type Mode string

const (
	ModePassphrase Mode = "passphrase"
	ModeKeys       Mode = "age keys"
)

// Encryption is a resolved set of recipients.
type Encryption struct {
	Mode       Mode
	Recipients []age.Recipient
}

// Encryption resolves recipients for a backup. Precedence: ForcePassphrase,
// explicit recipients, the public keys of configured identities, then a passphrase.
func (s *Source) Encryption() (*Encryption, error) {
	if !s.ForcePassphrase {
		var recips []age.Recipient
		for _, r := range s.Recipients {
			rec, err := age.ParseX25519Recipient(strings.TrimSpace(r))
			if err != nil {
				return nil, fmt.Errorf("recipient %q: %w", r, err)
			}
			recips = append(recips, rec)
		}
		for _, path := range s.RecipientFiles {
			f, err := os.Open(path)
			if err != nil {
				return nil, err
			}
			rs, err := age.ParseRecipients(f)
			_ = f.Close()
			if err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			recips = append(recips, rs...)
		}
		if len(recips) == 0 {
			ids, err := s.fileIdentities()
			if err != nil {
				return nil, err
			}
			for _, id := range ids {
				if x, ok := id.(*age.X25519Identity); ok {
					recips = append(recips, x.Recipient())
				}
			}
		}
		if len(recips) > 0 {
			return &Encryption{Mode: ModeKeys, Recipients: recips}, nil
		}
	}
	pass, err := s.passphrase("New vault passphrase", true)
	if err != nil {
		return nil, err
	}
	r, err := age.NewScryptRecipient(pass)
	if err != nil {
		return nil, err
	}
	return &Encryption{Mode: ModePassphrase, Recipients: []age.Recipient{r}}, nil
}

// PassphraseEncryption encrypts with a known passphrase (used to re-save an unlocked vault).
func PassphraseEncryption(pass string) (*Encryption, error) {
	r, err := age.NewScryptRecipient(pass)
	if err != nil {
		return nil, err
	}
	return &Encryption{Mode: ModePassphrase, Recipients: []age.Recipient{r}}, nil
}

// Unlocker holds the identities used for decryption and remembers the
// passphrase if one was asked for.
type Unlocker struct {
	src        *Source
	files      []age.Identity
	passphrase string
	asked      bool
}

// Unlocker loads identity files and prepares a lazy passphrase identity.
func (s *Source) Unlocker() (*Unlocker, error) {
	ids, err := s.fileIdentities()
	if err != nil {
		return nil, err
	}
	return &Unlocker{src: s, files: ids}, nil
}

// Identities returns the identities to pass to age. The passphrase is only
// requested if the file was encrypted with one.
func (u *Unlocker) Identities() []age.Identity {
	return append(append([]age.Identity(nil), u.files...), &lazyScrypt{u: u})
}

// Passphrase returns the passphrase used to unlock, if any.
func (u *Unlocker) Passphrase() (string, bool) { return u.passphrase, u.asked }

// Encryption returns recipients equivalent to how the vault was unlocked, so
// it can be saved again without asking.
func (u *Unlocker) Encryption() (*Encryption, error) {
	if u.asked {
		return PassphraseEncryption(u.passphrase)
	}
	return u.src.Encryption()
}

type lazyScrypt struct{ u *Unlocker }

func (l *lazyScrypt) Unwrap(stanzas []*age.Stanza) ([]byte, error) {
	found := false
	for _, s := range stanzas {
		if s.Type == "scrypt" {
			found = true
		}
	}
	if !found {
		return nil, age.ErrIncorrectIdentity
	}
	pass, err := l.u.src.passphrase("Vault passphrase", false)
	if err != nil {
		return nil, err
	}
	id, err := age.NewScryptIdentity(pass)
	if err != nil {
		return nil, err
	}
	key, err := id.Unwrap(stanzas)
	if err != nil {
		if errors.Is(err, age.ErrIncorrectIdentity) {
			return nil, ErrWrongPassphrase
		}
		return nil, err
	}
	l.u.passphrase, l.u.asked = pass, true
	return key, nil
}

func (s *Source) passphrase(prompt string, confirm bool) (string, error) {
	if s.Passphrase != "" {
		return s.Passphrase, nil
	}
	if p := os.Getenv(PassphraseEnv); p != "" {
		return p, nil
	}
	if s.Prompt == nil {
		return "", ErrNoPassphrase
	}
	p, err := s.Prompt(prompt, confirm)
	if err != nil {
		return "", err
	}
	if p == "" {
		return "", errors.New("empty passphrase")
	}
	return p, nil
}

func (s *Source) fileIdentities() ([]age.Identity, error) {
	var ids []age.Identity
	for _, path := range s.IdentityFiles {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		got, err := age.ParseIdentities(f)
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		ids = append(ids, got...)
	}
	return ids, nil
}

// Generate creates a new X25519 identity file at path (0600) and returns the public key.
func Generate(path string) (string, error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return "", err
	}
	content := fmt.Sprintf("# created by envboy keygen\n# public key: %s\n%s\n", id.Recipient(), id)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		return "", err
	}
	return id.Recipient().String(), f.Close()
}
