package main

import (
	"embed"
	"log"
	"os"
	"slices"

	"github.com/tailscale/tailcat"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

// backgroundFlag starts CatJump in the tray without showing its window, as
// used when it starts with the computer.
const backgroundFlag = "--background"

func main() {
	tailcat.Verbose = false
	a := NewApp()
	d := &desktop{}
	a.desktop = d

	app := application.New(application.Options{
		Name:        "CatJump",
		Description: "Share files directly, powered by tailcat",
		Services:    []application.Service{application.NewService(a)},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			// A menu bar app: no Dock icon, keeps running without windows.
			ActivationPolicy: application.ActivationPolicyAccessory,
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "dev.catjump.app",
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				// Launching CatJump again (or clicking a link / "Open with")
				// brings up the running instance.
				if !a.handleLaunchArgs(data.Args) {
					a.showWindow()
				}
			},
		},
	})
	d.app = app

	app.Event.OnApplicationEvent(events.Common.ApplicationLaunchedWithUrl, func(e *application.ApplicationEvent) {
		a.handleURL(e.Context().URL())
	})

	d.setup()
	launchedWithWork := a.handleLaunchArgs(os.Args[1:])

	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		if !launchedWithWork && !slices.Contains(os.Args[1:], backgroundFlag) {
			a.showWindow()
		}
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
