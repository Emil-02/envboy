package vault_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"

	"github.com/Emil-02/envboy/internal/keys"
	"github.com/Emil-02/envboy/internal/vault"
)

func sample() *vault.Vault {
	v := vault.New("envboy test")
	v.Entries = []vault.Entry{
		{Repo: "github.com/me/b", Path: "b", File: ".env", Content: []byte("B=2\n"), Keys: []string{"B"}},
		{Repo: "github.com/me/a", Path: "a", File: ".env", Content: []byte("A=1\n"), Keys: []string{"A"}},
	}
	v.Sort()
	return v
}

func TestRoundTripPassphrase(t *testing.T) {
	for _, armored := range []bool{true, false} {
		enc, err := keys.PassphraseEncryption("correct horse")
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if err := vault.Encrypt(&buf, sample(), enc.Recipients, armored); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(buf.Bytes(), []byte("A=1")) {
			t.Fatal("plaintext leaked into the vault")
		}
		if armored != strings.HasPrefix(buf.String(), "-----BEGIN AGE ENCRYPTED FILE-----") {
			t.Fatalf("armored=%v but output starts %q", armored, buf.String()[:20])
		}

		src := &keys.Source{Passphrase: "correct horse"}
		u, err := src.Unlocker()
		if err != nil {
			t.Fatal(err)
		}
		got, err := vault.Decrypt(bytes.NewReader(buf.Bytes()), u.Identities())
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Entries) != 2 || string(got.Entries[0].Content) != "A=1\n" {
			t.Fatalf("got %+v", got.Entries)
		}
		if p, ok := u.Passphrase(); !ok || p != "correct horse" {
			t.Fatal("unlocker should remember the passphrase")
		}

		bad := &keys.Source{Passphrase: "wrong"}
		bu, _ := bad.Unlocker()
		if _, err := vault.Decrypt(bytes.NewReader(buf.Bytes()), bu.Identities()); !errors.Is(err, keys.ErrWrongPassphrase) {
			t.Fatalf("want ErrWrongPassphrase, got %v", err)
		}
	}
}

func TestRoundTripKeysNeverPrompts(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "key.txt")
	pub, err := keys.Generate(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(keyFile); st.Mode().Perm() != 0o600 {
		t.Fatalf("key mode = %v", st.Mode().Perm())
	}
	if _, err := keys.Generate(keyFile); err == nil {
		t.Fatal("Generate must not overwrite an existing key")
	}

	prompted := false
	src := &keys.Source{IdentityFiles: []string{keyFile}, Prompt: func(string, bool) (string, error) {
		prompted = true
		return "", errors.New("should not prompt")
	}}
	enc, err := src.Encryption()
	if err != nil {
		t.Fatal(err)
	}
	if enc.Mode != keys.ModeKeys || enc.Recipients[0].(*age.X25519Recipient).String() != pub {
		t.Fatalf("expected the identity's own recipient, got %+v", enc)
	}

	path := filepath.Join(dir, "sub", "v.vault")
	if err := vault.Write(path, sample(), enc.Recipients, true); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("vault mode = %v", st.Mode().Perm())
	}
	u, err := src.Unlocker()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Read(path, u.Identities()); err != nil {
		t.Fatal(err)
	}
	if prompted {
		t.Fatal("prompted for a passphrase on a key-encrypted vault")
	}
}

func TestForcePassphraseAndEnv(t *testing.T) {
	t.Setenv(keys.PassphraseEnv, "from-env")
	src := &keys.Source{ForcePassphrase: true, Recipients: []string{"age1invalid"}}
	enc, err := src.Encryption()
	if err != nil {
		t.Fatal(err)
	}
	if enc.Mode != keys.ModePassphrase {
		t.Fatalf("mode = %v", enc.Mode)
	}
	t.Setenv(keys.PassphraseEnv, "")
	if _, err := (&keys.Source{}).Encryption(); !errors.Is(err, keys.ErrNoPassphrase) {
		t.Fatalf("want ErrNoPassphrase, got %v", err)
	}
}

func TestReplaceMerge(t *testing.T) {
	v := sample()
	fresh := []vault.Entry{{Repo: "github.com/me/a", File: ".env.local", Content: []byte("X=1")}}
	v.Replace(fresh, map[string]bool{"github.com/me/a": true}, true)
	if len(v.Entries) != 2 {
		t.Fatalf("merge: %+v", v.Entries)
	}
	for _, e := range v.Entries {
		if e.Repo == "github.com/me/a" && e.File == ".env" {
			t.Fatal("a/.env was deleted on disk and should be dropped")
		}
	}
	v.Replace(fresh, map[string]bool{"github.com/me/a": true}, false)
	if len(v.Entries) != 1 {
		t.Fatalf("prune: %+v", v.Entries)
	}
}

func TestRejectsFutureVersion(t *testing.T) {
	v := sample()
	v.Version = 99
	enc, _ := keys.PassphraseEncryption("p")
	var buf bytes.Buffer
	if err := vault.Encrypt(&buf, v, enc.Recipients, false); err != nil {
		t.Fatal(err)
	}
	u, _ := (&keys.Source{Passphrase: "p"}).Unlocker()
	if _, err := vault.Decrypt(&buf, u.Identities()); err == nil || !strings.Contains(err.Error(), "version 99") {
		t.Fatalf("want version error, got %v", err)
	}
}
