//go:build !linux && !windows

package main

// registerProtocol is a no-op on macOS: the scheme is declared in the app
// bundle's Info.plist (see "protocols" in wails.json) and Launch Services
// registers it when the app is first opened.
func registerProtocol() {}
