// Command certwatch is the certwatch command-line interface.
package main

import (
	"os"

	"github.com/titiabdelghafour8/certwatch/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
