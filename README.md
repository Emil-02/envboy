# envboy 🤠

**Back up, restore, check and guard every `.env` file across all your repos.** One Go binary, no server, no account.

🇮🇩 [Baca dalam Bahasa Indonesia](README.id.md)

> **Using an AI coding agent?** Install [`jaga-skill`](https://github.com/Emil-02/jaga/tree/main/skills/jaga-skill) with `npx jaga-skill init` — a gate that stops the agent before commit/deploy when secrets or personal data are detected. It also guards `.env` files.

```console
$ envboy backup ~/Proyek
✓ Backed up 23 files from 14 repos to ~/envboy.vault (passphrase)

# new laptop, repos cloned wherever you like
$ envboy restore ~/code
╭──────────┬────────────────────────┬────────────┬──────────────────────╮
│ Action   │ Repo                   │ File       │ Where / notes        │
├──────────┼────────────────────────┼────────────┼──────────────────────┤
│ + create │ github.com/me/campus   │ .env       │ ~/code/campus-hub    │
│ + create │ github.com/me/temuin   │ .env.local │ ~/code/temuin        │
│ ...                                                                    │
```

`.env` files are the one part of a project that git is supposed to forget. That is right for security, but it means that
reinstalling or switching laptops leaves you copying dozens of files by hand from a USB stick, a chat with yourself, or an old
`restore-env.sh`. A secret can also slip into a commit.

envboy handles all of it:

| Command | What it does |
|---|---|
| `envboy scan` | Finds every env file under your folders and flags the ones that are **committed** or **not in `.gitignore`** |
| `envboy backup` | Encrypts all of them into **one [age](https://age-encryption.org) vault** you can keep anywhere |
| `envboy restore` | Puts each file back into the right repo, **matched by git remote**, so folder names and layout can change |
| `envboy status` | Shows what changed since the last backup (key names only, never values) |
| `envboy check` | Compares `.env` with `.env.example`: missing, extra and empty keys. CI-friendly exit codes |
| `envboy guard` | Installs a pre-commit hook that blocks env files and fixes `.gitignore` |
| `envboy` | Opens an interactive dashboard with all of the above |

## Install

```sh
go install github.com/Emil-02/envboy@latest
```

Or download a binary from [Releases](https://github.com/Emil-02/envboy/releases) (Linux, macOS, Windows; amd64 and arm64).

## Quick start

```sh
envboy scan ~/Proyek            # what is there, and is any of it leaking?
envboy backup ~/Proyek          # asks for a passphrase, writes ~/envboy.vault
# copy ~/envboy.vault to a USB stick, cloud drive, or a private repo

# on the new machine, after cloning your repos anywhere:
envboy restore ~/code --dry-run # see the plan
envboy restore ~/code           # write the files (asks to confirm)
```

Run `envboy` with no arguments for the dashboard:

```
 envboy ~/Proyek                                              ~/envboy.vault 🔓 23 files
╭────────────────────────────────╮╭──────────────────────────────────────────────────────╮
│ Amboyy_Trade-AI            ✓ 1 ││ kos-keluarga                                         │
│ Temuin                     ✓ 1 ││ ~/Proyek/kos-keluarga                                │
│ kos-keluarga           !2 ● 1  ││ github.com/emil-02/kos-keluarga                      │
│ umkm-kit                 ⚠ ✓ 1 ││ ✓ pre-commit guard installed                         │
│ Proyek_Fotografer         ↓1 0 ││                                                      │
│                                ││ Env files                                            │
│                                ││  .env.local ● changed since backup  9 keys           │
│                                ││    FONNTE_TOKEN, NEXT_PUBLIC_SUPABASE_URL, RESEND_…  │
│                                ││    only on disk: CRON_SECRET                         │
│                                ││                                                      │
│                                ││ Check against templates                              │
│                                ││  .env.local ↔ .env.example                           │
│                                ││    missing: MIDTRANS_SERVER_KEY, QRIS_MERCHANT_ID    │
╰────────────────────────────────╯╰──────────────────────────────────────────────────────╯
 ↑↓ move  / filter  b backup  R restore  f fix .gitignore  h hook  u unlock  r rescan  ? help  q quit
```

## Encryption: passphrase or age keys

**Passphrase (default).** You only need to remember one secret. envboy asks for it, or reads `ENVBOY_PASSPHRASE` in scripts.
It uses age's scrypt mode.

**age keys.** No passphrase to type, and teammates can share a vault:

```sh
envboy keygen                   # writes ~/.config/envboy/key.txt and prints your public key
envboy backup ~/Proyek          # now encrypts to that key automatically
envboy backup -r age1teammate… -r age1you…  # or encrypt to several people
envboy restore -i ~/key.txt     # use a specific identity file
```

Back up `key.txt` itself, for example in a password manager. Without it, a key-encrypted vault cannot be opened. The vault is
a standard age file, so `age -d` can decrypt it even without envboy.

## How restore finds the right repo

Each backed-up file records the repo's **normalized remote** (`git@github.com:Me/App.git` and `https://github.com/me/app`
both become `github.com/me/app`) plus its path relative to the scanned folder.

1. If a local repo has the same remote, that repo is used, wherever it is.
2. If several local clones share the remote, the one at the same relative path wins, then the one with the same folder name.
   If none of them fits, the file is reported as *ambiguous* and skipped.
3. Repos without a remote, and plain folders, are matched by relative path. A different repository that now sits at the old
   path is never written into.

Local files that differ from the vault are **never overwritten by default**. `--backup-existing` moves them to `.env.bak`
first, and `--force` overwrites them. Restored files get `0600` permissions.

## Check against templates

```console
$ envboy check ~/Proyek
╭──────────────┬─────────────────────────┬──────────────────────────────────╮
│ Repo         │ File                    │ Problems                         │
├──────────────┼─────────────────────────┼──────────────────────────────────┤
│ kos-keluarga │ .env.local ↔ .env.example │ missing: MIDTRANS_SERVER_KEY   │
│              │                         │ empty: RESEND_API_KEY            │
╰──────────────┴─────────────────────────┴──────────────────────────────────╯
```

Each env file is compared with `<name>.example` (or `.sample`, `.template`, `.dist`) if there is one, otherwise with
`.env.example` in the same folder. `check` exits with `1` when keys are missing, so you can use it in a `predev` script or in CI.
`--strict` also fails on extra keys, empty values, and templates that have no env file.

## Guard

```sh
envboy guard install            # this repo
envboy guard install --all ~/Proyek   # every repo below ~/Proyek
envboy guard fix ~/Proyek       # add ignore rules where env files are not ignored
envboy guard fix --untrack      # also `git rm --cached` env files that were committed
```

The hook respects `core.hooksPath`. An existing `pre-commit` hook is kept and runs after envboy's check, and
`guard uninstall` puts it back. Templates such as `.env.example` stay committable.

## What counts as an env file

`.env`, `.env.*` and `*.env`, except templates (`*.example`, `*.sample`, `*.template`, `*.dist`, `*.tpl`) and backups
(`*.bak`, `*.orig`, `*~`). Folders such as `node_modules`, `.venv`, `vendor`, `.next`, `dist` and `build` are skipped. Files over
1 MiB are listed but not read. All of this can be changed in the [config](.envboy.example.yml).

## Configuration

Optional. Put it at `~/.config/envboy/config.yml`:

```yaml
roots: [~/Proyek, ~/Projects]   # scanned when you pass no directory
vault: ~/Dropbox/envboy.vault
recipients: [age1…]             # encrypt to these keys instead of a passphrase
skip_dirs: [archive]
```

See [.envboy.example.yml](.envboy.example.yml) for every option.

## Scripting

Every command accepts `--json`. Exit codes:

| Code | Meaning |
|---|---|
| 0 | OK |
| 1 | Error, or `check` found missing keys, or `guard run` blocked a commit |
| 2 | `scan` found an env file committed to git |
| 3 | `status`: the vault and the disk differ |

## Security

* Values are never printed. `scan`, `status`, `check`, the dashboard and `--json` show key names only.
* The vault is a standard age file (ASCII-armored by default; `--binary` writes binary age). It is written atomically
  with `0600` permissions.
* A key-encrypted vault never asks for a passphrase.

Read [SECURITY.md](SECURITY.md) for the threat model and how to report a vulnerability.

## Development

```sh
make test     # unit + end-to-end tests (needs git)
make race
make lint     # golangci-lint
make fuzz     # fuzz the .env parser
```

## License

[MIT](LICENSE)
