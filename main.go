package main

import (
	"os"

	"github.com/zfg5118/deeprouter-setup/internal/cli"
)

// version is overridden by -ldflags "-X main.version=..." in install.sh.
var version = "0.1.0"

func main() {
	os.Exit(cli.Main(os.Args[1:], version))
}
