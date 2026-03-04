// Package main is the entrypoint for the savecraft daemon.
package main

import (
	"os"

	"github.com/joshsymonds/savecraft.gg/cmd/savecraftd/cmd"
)

var version = "dev"
var serverURLDefault = "https://api.savecraft.gg"
var installURLDefault = "https://install.savecraft.gg"
var appName = "savecraft"
var statusPortDefault = "9182"
var frontendURLDefault = "https://savecraft.gg"

func main() {
	if err := cmd.Execute(
		version, serverURLDefault, installURLDefault,
		appName, statusPortDefault, frontendURLDefault,
	); err != nil {
		os.Exit(1)
	}
}
