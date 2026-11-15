# Contributing

Thanks for helping! Issues and pull requests are welcome, in English or Bahasa Indonesia.

## Setup

```sh
git clone https://github.com/Emil-02/envboy && cd envboy
make test   # needs Go (see go.mod) and git
make build && ./envboy --help
```

## Layout

* `cmd/`: thin cobra commands. Only flag parsing and output live here.
* `internal/discover`: finds repos and env files. `gitinfo` wraps git, and `dotenv` parses key names (never values).
* `internal/vault`, `internal/keys`: the encrypted format and how keys and passphrases are resolved.
* `internal/restore`, `check`, `guard`, `backup`: one feature each, all testable without the CLI.
* `internal/tui`: the Bubble Tea dashboard.

## Guidelines

* **Never print or log a value from an env file.** Tests should check this whenever output changes.
* New behaviour needs a test. End-to-end CLI tests live in `cmd/cmd_test.go` and use real temporary git repos.
* Run `make lint` before you push. CI runs tests on Linux, macOS and Windows.
* Keep commits focused. Use conventional prefixes (`feat:`, `fix:`, `docs:`).
* A change to the vault format must bump `vault.FormatVersion` and stay able to read older versions.
