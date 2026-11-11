package main

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// registerProtocol registers catjump:// links for the current user
// (HKCU, no admin rights needed), so the portable .exe works without the
// installer. The NSIS installer registers the same keys.
func registerProtocol() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	exe, _ = filepath.Abs(exe)
	if strings.Contains(strings.ToLower(exe), `\temp\`) {
		return
	}
	base := `Software\Classes\` + linkScheme
	k, _, err := registry.CreateKey(registry.CURRENT_USER, base, registry.ALL_ACCESS)
	if err != nil {
		return
	}
	k.SetStringValue("", "URL:CatJump")
	k.SetStringValue("URL Protocol", "")
	k.Close()
	if k, _, err := registry.CreateKey(registry.CURRENT_USER, base+`\DefaultIcon`, registry.ALL_ACCESS); err == nil {
		k.SetStringValue("", `"`+exe+`",0`)
		k.Close()
	}
	if k, _, err := registry.CreateKey(registry.CURRENT_USER, base+`\shell\open\command`, registry.ALL_ACCESS); err == nil {
		k.SetStringValue("", `"`+exe+`" "%1"`)
		k.Close()
	}
}
