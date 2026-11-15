// Command envboy backs up, restores, checks and guards .env files across many repos.
package main

import (
	"os"

	"github.com/Emil-02/envboy/cmd"
)

var version = "dev"

func main() {
	cmd.Version = version
	os.Exit(cmd.Execute())
}
