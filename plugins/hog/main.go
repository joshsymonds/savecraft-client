// Package main is a test plugin that allocates unbounded memory. Used to
// verify the runner's WASM memory limit causes a graceful failure instead of
// OOM-killing the long-lived daemon.
package main

func main() {
	var keep [][]byte
	for {
		// 8 MiB chunks, touched every page so the Wasm linear memory is
		// actually grown/committed rather than lazily reserved.
		b := make([]byte, 8<<20)
		for i := 0; i < len(b); i += 4096 {
			b[i] = 1
		}
		keep = append(keep, b)
	}
}
