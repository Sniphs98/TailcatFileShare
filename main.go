package main

import (
	"embed"
	"os"

	"github.com/tailscale/tailcat"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var icon []byte

func main() {
	tailcat.Verbose = false
	app := NewApp()
	app.handleLaunchArgs(os.Args[1:])

	err := wails.Run(&options.App{
		Title:     "CatJump",
		Width:     1080,
		Height:    720,
		MinWidth:  760,
		MinHeight: 560,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 255},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: true,
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "dev.catjump.app.7f3c1e",
			OnSecondInstanceLaunch: func(d options.SecondInstanceData) {
				app.handleLaunchArgs(d.Args)
				if app.ctx != nil {
					runtime.WindowUnminimise(app.ctx)
					runtime.WindowShow(app.ctx)
				}
			},
		},
		Bind: []interface{}{app},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
			About: &mac.AboutInfo{
				Title:   "CatJump",
				Message: "Share files directly and end-to-end encrypted, powered by tailcat.",
				Icon:    icon,
			},
			OnUrlOpen: app.handleURL,
		},
		Linux: &linux.Options{
			Icon:        icon,
			ProgramName: "catjump",
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}

func bringToFront(a *App) {
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowShow(a.ctx)
}
