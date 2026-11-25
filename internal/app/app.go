// Package app wires the Wails application: tray icon, popup window and the
// service the frontend talks to.
package app

import (
	"context"
	"log/slog"
	"os"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/icons"

	"github.com/ethan2047rr/kikri/internal/gui"
)

const (
	popupWidth  = 360
	popupHeight = 480
)

// Run starts the tray app and blocks until it quits.
func Run(version string) error {
	var app *application.App
	svc := NewService(version, func(s Snapshot) {
		if app != nil {
			app.Event.Emit(EventSnapshot, s)
		}
	})

	logLevel := slog.LevelInfo
	if os.Getenv("KIKRI_DEBUG") != "" {
		logLevel = slog.LevelDebug
	}

	app = application.New(application.Options{
		Name:        "Kikri",
		LogLevel:    logLevel,
		Description: "Pre-warm AI coding CLI quota windows",
		Services:    []application.Service{application.NewService(svc)},
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(gui.Assets()),
		},
		Mac: application.MacOptions{
			// Menu bar only: no Dock icon, no app menu.
			ActivationPolicy: application.ActivationPolicyAccessory,
		},
		Windows: application.WindowsOptions{
			DisableQuitOnLastWindowClosed: true,
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "io.github.ethan2047rr.kikri",
		},
	})

	popup := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:            "popup",
		Title:           "Kikri",
		Width:           popupWidth,
		Height:          popupHeight,
		Frameless:       true,
		AlwaysOnTop:     true,
		Hidden:          true,
		DisableResize:   true,
		HideOnEscape:    true,
		HideOnFocusLost: true,
		URL:             "/",
		Windows: application.WindowsWindow{
			HiddenOnTaskbar: true,
		},
	})
	// Closing the popup only hides it; Kikri keeps running in the tray.
	popup.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		popup.Hide()
		e.Cancel()
	})

	tray := app.SystemTray.New()
	tray.SetTooltip("Kikri")
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(icons.SystrayMacTemplate) // TODO(M4): rooster icon
	} else {
		tray.SetIcon(icons.SystrayLight)
		tray.SetDarkModeIcon(icons.SystrayDark)
	}

	menu := app.NewMenu()
	menu.Add("Kikri " + version).SetEnabled(false)
	menu.AddSeparator()
	menu.Add("重新检测").OnClick(func(*application.Context) {
		go svc.Refresh(context.Background())
	})
	menu.AddSeparator()
	menu.Add("退出 Kikri").OnClick(func(*application.Context) { app.Quit() })
	tray.SetMenu(menu)
	tray.AttachWindow(popup).WindowOffset(5)

	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go func() {
			svc.Prepare()
			svc.Refresh(context.Background())
		}()
	})

	return app.Run()
}
