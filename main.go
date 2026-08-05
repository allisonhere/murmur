// Command murmur captures rough thoughts into an Obsidian vault.
package main

import (
	"os"

	"github.com/alliebayless/murmur/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
