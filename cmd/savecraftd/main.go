// Package main is the entrypoint for the savecraft daemon.
package main

import (
	"os"

	"github.com/joshsymonds/savecraft.gg/cmd/savecraftd/cmd"
)

var version = "dev"
var serverURLDefault = "https://api.savecraft.gg"
var installURLDefault = "https://install.savecraft.gg"

func main() {
	if err := cmd.Execute(version, serverURLDefault, installURLDefault); err != nil {
		os.Exit(1)
	}
}
