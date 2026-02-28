// Package main is the entrypoint for the savecraft daemon.
package main

import (
	"os"

	"github.com/joshsymonds/savecraft.gg/cmd/savecraftd/cmd"
)

var version = "dev"

func main() {
	if err := cmd.Execute(version); err != nil {
		os.Exit(1)
	}
}
