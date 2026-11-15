package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

func stdinIsTerminal() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// promptSecret reads a passphrase without echo. With confirm it asks twice.
func promptSecret(prompt string, confirm bool) (string, error) {
	if !stdinIsTerminal() {
		return "", errors.New("a passphrase is required: set ENVBOY_PASSPHRASE or run in a terminal")
	}
	read := func(p string) (string, error) {
		fmt.Fprint(os.Stderr, p+": ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
	first, err := read(prompt)
	if err != nil {
		return "", err
	}
	if !confirm {
		return first, nil
	}
	second, err := read("Repeat passphrase")
	if err != nil {
		return "", err
	}
	if first != second {
		return "", errors.New("passphrases do not match")
	}
	return first, nil
}

// confirm asks a yes/no question; non-interactive sessions get def.
func confirm(question string, def bool) bool {
	if !stdinIsTerminal() {
		return def
	}
	hint := "[y/N]"
	if def {
		hint = "[Y/n]"
	}
	fmt.Fprintf(os.Stderr, "%s %s ", question, hint)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "ya":
		return true
	case "n", "no", "tidak":
		return false
	}
	return def
}
