// Package main is a test plugin that never terminates. Used to verify the
// runner's per-parse timeout and WithCloseOnContextDone behaviour.
package main

func main() {
	for {
	}
}
