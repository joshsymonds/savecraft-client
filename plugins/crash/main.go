// Package main is a test plugin that exits non-zero without producing ndjson output.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "something went wrong")
	const exitCode = 2
	os.Exit(exitCode)
}
