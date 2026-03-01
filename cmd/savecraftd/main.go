// Package main is the entrypoint for the savecraft daemon.
package main

import (
	"os"

	"github.com/joshsymonds/savecraft.gg/cmd/savecraftd/cmd"
)

var version = "dev"
var serverURLDefault = "https://api.savecraft.gg"

func main() {
	if err := cmd.Execute(version, serverURLDefault); err != nil {
		os.Exit(1)
	}
}
