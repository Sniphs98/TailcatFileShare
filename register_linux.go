package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// registerProtocol makes this binary the handler for katzensprung:// links
// for the current user, via a .desktop file and xdg-mime. It runs on every
// start so the registration follows the binary if it is moved.
func registerProtocol() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	exe, _ = filepath.EvalSymlinks(exe)
	if strings.Contains(exe, string(filepath.Separator)+"tmp"+string(filepath.Separator)) {
		return // `wails dev` or go run: don't register a throwaway binary
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	appsDir := filepath.Join(dataHome, "applications")
	if err := os.MkdirAll(appsDir, 0o755); err != nil {
		return
	}
	iconPath := filepath.Join(dataHome, "icons", "hicolor", "512x512", "apps", "katzensprung.png")
	if os.MkdirAll(filepath.Dir(iconPath), 0o755) == nil {
		_ = os.WriteFile(iconPath, icon, 0o644)
	}
	desktop := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=Katzensprung
Comment=Dateien direkt mit Freunden teilen
Exec="%s" %%U
Icon=katzensprung
Terminal=false
Categories=Network;FileTransfer;
MimeType=x-scheme-handler/%s;application/octet-stream;
StartupWMClass=katzensprung
`, exe, linkScheme)
	file := filepath.Join(appsDir, "katzensprung.desktop")
	if old, err := os.ReadFile(file); err == nil && string(old) == desktop {
		return
	}
	if err := os.WriteFile(file, []byte(desktop), 0o644); err != nil {
		return
	}
	_ = exec.Command("xdg-mime", "default", "katzensprung.desktop", "x-scheme-handler/"+linkScheme).Run()
	_ = exec.Command("update-desktop-database", appsDir).Run()
}
