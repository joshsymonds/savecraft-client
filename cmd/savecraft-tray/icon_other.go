//go:build !windows

package main

import _ "embed"

//go:embed assets/icon.png
var iconBytes []byte
