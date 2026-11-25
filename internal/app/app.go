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

	"github.com/ethan2047rr/kikri/internal/gui"
	"github.com/ethan2047rr/kikri/internal/locale"
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
	// Opening the popup refreshes the official usage numbers (at most once
	// a minute; the query is read-only and never opens a window).
	popup.OnWindowEvent(events.Common.WindowShow, func(*application.WindowEvent) {
		go svc.RefreshQuota(false)
	})
	// Closing the popup only hides it; Kikri keeps running in the tray.
	popup.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		popup.Hide()
		e.Cancel()
	})

	tray := app.SystemTray.New()
	tray.SetTooltip("Kikri")
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(trayTemplateIcon)
	} else {
		tray.SetIcon(trayLightIcon)
		tray.SetDarkModeIcon(trayDarkIcon)
	}

	menu := app.NewMenu()
	menu.Add("Kikri " + version).SetEnabled(false)
	menu.AddSeparator()
	redetect := menu.Add(trayLabel(svc.Snapshot().Lang, "redetect")).OnClick(func(*application.Context) {
		go svc.Refresh(context.Background())
	})
	menu.AddSeparator()
	quit := menu.Add(trayLabel(svc.Snapshot().Lang, "quit")).OnClick(func(*application.Context) { app.Quit() })
	svc.onLang = func(lang string) {
		redetect.SetLabel(trayLabel(lang, "redetect"))
		quit.SetLabel(trayLabel(lang, "quit"))
		menu.Update()
	}
	svc.quit = app.Quit
	tray.SetMenu(menu)
	tray.AttachWindow(popup).WindowOffset(5)

	schedCtx, stopScheduler := context.WithCancel(context.Background())
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go func() {
			svc.Prepare()
			svc.startNotifications()
			svc.StartScheduler(schedCtx)
			svc.Refresh(context.Background())
		}()
	})
	// After sleep, check right away instead of waiting for the next tick:
	// missed slots are made up as soon as the network is back (F4.1).
	app.Event.OnApplicationEvent(events.Common.SystemDidWake, func(*application.ApplicationEvent) {
		svc.Kick()
	})

	err := app.Run()
	stopScheduler()
	svc.Close()
	return err
}

// trayLabel is a tray menu item's text in the UI language.
func trayLabel(lang, key string) string {
	labels := map[string][2]string{ // zh, en
		"redetect": {"重新检测", "Re-detect"},
		"quit":     {"退出 Kikri", "Quit Kikri"},
	}
	if lang == locale.En {
		return labels[key][1]
	}
	return labels[key][0]
}
