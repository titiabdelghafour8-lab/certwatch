// Command certwatch is the certwatch command-line interface.
package main

import (
	"os"

	"github.com/titiabdelghafour8-lab/certwatch/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
