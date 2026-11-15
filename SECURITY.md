# Security

## Reporting a vulnerability

Please report vulnerabilities privately through
[GitHub Security Advisories](https://github.com/Emil-02/envboy/security/advisories/new). Do not open a public issue.
You should get a reply within a week.

## Threat model

envboy protects env files **at rest in the vault** and **from accidental commits**. It is not a secrets manager and does not
protect secrets on a machine that is already compromised.

**What the vault protects against.** Anyone who gets the vault file but not the passphrase or identity cannot read it. The
vault is a standard [age](https://age-encryption.org) file. Passphrase mode uses age's scrypt recipient, and key mode uses
X25519. envboy does not implement any cryptography itself. Repo names, file paths and key names are stored *inside* the
encrypted payload, so they are hidden too.

**What it does not protect against.**

* A weak passphrase. scrypt slows down guessing, but a short or reused passphrase can still be brute-forced. Use a long one,
  or use `envboy keygen`.
* Losing the passphrase or `key.txt`. Nobody can recover the vault without them.
* `ENVBOY_PASSPHRASE`. Environment variables can be read by other processes of the same user and may end up in shell history.
  Prefer the prompt or age keys on shared machines.
* Secrets that were already committed. `guard fix --untrack` stops tracking the file, but the secret stays in git history.
  Rotate it.
* A malicious vault. Restore refuses entries whose file paths leave the repository. It writes only into repositories that
  already exist locally, and by default it never overwrites a differing file. Still, only restore vaults you created.

## Guarantees in the code

* Values are never printed: commands, the dashboard and `--json` output show key names only.
* The vault and restored files are written atomically (temp file + rename) with `0600` permissions.
* The pre-commit hook only *blocks*. It never modifies the index.
* envboy makes no network connections.
