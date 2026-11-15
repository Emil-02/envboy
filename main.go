// Command envboy backs up, restores, checks and guards .env files across many repos.
package main

import (
	"os"
	"runtime/debug"

	"github.com/Emil-02/envboy/cmd"
)

var version = "dev"

func main() {
	// `go install module@vX` builds without ldflags; the module version is in the build info.
	if version == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		}
	}
	cmd.Version = version
	os.Exit(cmd.Execute())
}
