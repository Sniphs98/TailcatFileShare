package main

import (
	_ "embed"
	"runtime"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed build/appicon.png
var icon []byte

//go:embed build/tray.png
var trayIcon []byte

//go:embed build/tray-mac-template.png
var trayIconMac []byte

const (
	windowWidth  = 400
	windowHeight = 620
)

// desktop is the window-system side of CatJump: a compact window attached
// to an icon in the system tray (menu bar on macOS).
type desktop struct {
	app  *application.App
	win  *application.WebviewWindow
	tray *application.SystemTray
}

func (d *desktop) setup() {
	d.win = d.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "CatJump",
		Width:            windowWidth,
		Height:           windowHeight,
		Frameless:        true,
		AlwaysOnTop:      true, // stays above Explorer/Finder while files are dragged in
		Hidden:           true,
		DisableResize:    true,
		HideOnEscape:     true,
		EnableFileDrop:   true,
		BackgroundColour: application.NewRGB(0, 0, 0),
		URL:              "/",
		Windows: application.WindowsWindow{
			HiddenOnTaskbar: true,
		},
	})

	// Closing the window (Alt+F4, Cmd+W) only hides it; CatJump keeps
	// running in the tray so shares stay online.
	d.win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		d.win.Hide()
		e.Cancel()
	})
	d.win.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
		if files := e.Context().DroppedFiles(); len(files) > 0 {
			d.emit("files-dropped", files)
		}
	})

	menu := application.NewMenu()
	menu.Add("Open CatJump").OnClick(func(*application.Context) { d.show() })
	menu.Add("Share files…").OnClick(func(*application.Context) {
		d.show()
		d.emit("pick-files", nil)
	})
	menu.AddSeparator()
	menu.Add("Quit CatJump").OnClick(func(*application.Context) { d.app.Quit() })

	d.tray = d.app.SystemTray.New()
	d.tray.SetTooltip("CatJump")
	if runtime.GOOS == "darwin" {
		d.tray.SetTemplateIcon(trayIconMac)
	} else {
		d.tray.SetIcon(trayIcon)
	}
	d.tray.SetMenu(menu)
	d.tray.AttachWindow(d.win).WindowOffset(8).WindowDebounce(200 * time.Millisecond)
}

func (d *desktop) emit(name string, data any) {
	d.app.Event.Emit(name, data)
}

// show opens the window next to the tray icon (or centered where the
// desktop can't tell where the icon is).
func (d *desktop) show() {
	if d.tray == nil {
		d.win.Show().Focus()
		return
	}
	d.tray.ShowWindow()
}

func (d *desktop) hide() { d.win.Hide() }

func (d *desktop) openDialog(title, dir string, folders bool) *application.OpenFileDialogStruct {
	dlg := d.app.Dialog.OpenFile().
		SetTitle(title).
		CanChooseFiles(!folders).
		CanChooseDirectories(folders).
		CanCreateDirectories(folders).
		AttachToWindow(d.win)
	if dir != "" {
		dlg.SetDirectory(dir)
	}
	return dlg
}
