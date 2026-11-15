# Changelog

## v0.1.0

First release.

* `scan`: find env files across many repos and flag committed or unignored ones.
* `backup`: write one age-encrypted vault (passphrase or age keys) that merges across machines.
* `restore`: match repos by normalized git remote, with path fallback, a dry-run plan, `--force` and `--backup-existing`.
* `status`: compare the vault with the disk by key names.
* `check`: compare env files with `.env.example` and similar templates, with `--strict` for CI.
* `guard`: a chained pre-commit hook, `.gitignore` fixes, and `--untrack`.
* `keygen`: create an age identity that is used automatically.
* An interactive dashboard (`envboy` with no arguments).
* `--json` output and documented exit codes.
