module github.com/joshsymonds/savecraft.gg

go 1.26.0

require (
	fyne.io/systray v1.12.0
	github.com/BurntSushi/toml v1.6.0
	github.com/coder/websocket v1.8.14
	github.com/fsnotify/fsnotify v1.9.0
	github.com/spf13/cobra v1.10.2
	github.com/tetratelabs/wazero v1.11.0
	golang.org/x/sys v0.41.0
	golang.org/x/term v0.40.0
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jchv/go-webview2 v0.0.0-20260205173254-56598839c808
	github.com/jchv/go-winloader v0.0.0-20250406163304-c1995be93bd1 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
)

replace github.com/jchv/go-webview2 => github.com/joshsymonds/go-webview2 v0.0.0-20260313235041-e1c281c87318
