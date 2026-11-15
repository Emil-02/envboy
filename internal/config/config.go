// Package config loads envboy's YAML configuration and fills in defaults.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the on-disk configuration (~/.config/envboy/config.yml).
type Config struct {
	// Roots are scanned when a command gets no path arguments.
	Roots []string `yaml:"roots"`
	// Vault is the default vault path.
	Vault string `yaml:"vault"`
	// Recipients are age public keys (age1...) used for backup instead of a passphrase.
	Recipients []string `yaml:"recipients"`
	// Identities are age identity files used to decrypt the vault.
	Identities []string `yaml:"identities"`
	// Patterns are basename globs that mark a file as an env file.
	Patterns []string `yaml:"patterns"`
	// Exclude are basename globs that are never env files (editor and restore backups).
	Exclude []string `yaml:"exclude"`
	// TemplateMarkers are dot-separated name segments that mark a file as a
	// committed template (".env.example") rather than a secret.
	TemplateMarkers []string `yaml:"template_markers"`
	// SkipDirs are extra directory names never descended into (added to the defaults).
	SkipDirs []string `yaml:"skip_dirs"`
}

var (
	DefaultPatterns        = []string{".env", ".env.*", "*.env"}
	DefaultExclude         = []string{"*.bak", "*.bak.*", "*.orig", "*.swp", "*~"}
	DefaultTemplateMarkers = []string{"example", "sample", "template", "dist", "tpl"}
	DefaultSkipDirs        = []string{
		".git", "node_modules", ".venv", "venv", "__pycache__", ".next", ".nuxt", ".svelte-kit",
		"dist", "build", "out", "target", "vendor", ".dart_tool", ".gradle", ".idea", ".cache",
		".terraform", "Pods", ".pytest_cache", ".ruff_cache", ".mypy_cache", ".tox", ".turbo",
		".vercel", ".expo", "coverage",
	}
)

// Default returns the built-in configuration.
func Default() Config {
	home, _ := os.UserHomeDir()
	c := Config{
		Vault:           filepath.Join(home, "envboy.vault"),
		Patterns:        append([]string(nil), DefaultPatterns...),
		Exclude:         append([]string(nil), DefaultExclude...),
		TemplateMarkers: append([]string(nil), DefaultTemplateMarkers...),
		SkipDirs:        append([]string(nil), DefaultSkipDirs...),
	}
	if id := DefaultIdentityPath(); fileExists(id) {
		c.Identities = []string{id}
	}
	return c
}

// Dir is envboy's configuration directory.
func Dir() string {
	if d := os.Getenv("ENVBOY_CONFIG_DIR"); d != "" {
		return d
	}
	d, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		d = filepath.Join(home, ".config")
	}
	return filepath.Join(d, "envboy")
}

// Path is the default config file path.
func Path() string { return filepath.Join(Dir(), "config.yml") }

// DefaultIdentityPath is where `envboy keygen` writes the age identity.
func DefaultIdentityPath() string { return filepath.Join(Dir(), "key.txt") }

// Load reads the config at path (or the default path when empty). A missing
// default file is not an error: the defaults are returned.
func Load(path string) (Config, error) {
	c := Default()
	explicit := path != ""
	if !explicit {
		path = Path()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !explicit {
			return c, nil
		}
		return c, fmt.Errorf("read config: %w", err)
	}
	var file Config
	if err := yaml.Unmarshal(data, &file); err != nil {
		return c, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(file.Roots) > 0 {
		c.Roots = file.Roots
	}
	if file.Vault != "" {
		c.Vault = file.Vault
	}
	if len(file.Recipients) > 0 {
		c.Recipients = file.Recipients
	}
	if len(file.Identities) > 0 {
		c.Identities = file.Identities
	}
	if len(file.Patterns) > 0 {
		c.Patterns = file.Patterns
	}
	if file.Exclude != nil {
		c.Exclude = file.Exclude
	}
	if len(file.TemplateMarkers) > 0 {
		c.TemplateMarkers = file.TemplateMarkers
	}
	c.SkipDirs = append(c.SkipDirs, file.SkipDirs...)

	c.Vault = Expand(c.Vault)
	for i := range c.Roots {
		c.Roots[i] = Expand(c.Roots[i])
	}
	for i := range c.Identities {
		c.Identities[i] = Expand(c.Identities[i])
	}
	return c, nil
}

// Expand replaces a leading ~ with the home directory.
func Expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}
