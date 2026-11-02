// Command scn3 is the desktop app: a sunburst view of disk usage.
//
// Wails builds the package in the project root, so this main lives here and
// not under cmd/.
package main

import (
	"context"
	"embed"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/mnorrsken/scn3/internal/desktop"
)

//go:embed all:frontend/dist
var assets embed.FS

// Dialogs holds the calls that need the Wails context.
type Dialogs struct{ ctx context.Context }

// ChooseFolder asks for a folder to scan. It returns "" if cancelled.
func (d *Dialogs) ChooseFolder() (string, error) {
	return runtime.OpenDirectoryDialog(d.ctx, runtime.OpenDialogOptions{Title: "Choose a folder to scan"})
}

// OpenPrivacySettings opens the macOS Full Disk Access settings page.
func (d *Dialogs) OpenPrivacySettings() {
	runtime.BrowserOpenURL(d.ctx, "x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles")
}

func main() {
	app := desktop.New(func(string, any) {})
	dlg := &Dialogs{}
	settings := desktop.LoadSettings()

	err := wails.Run(&options.App{
		Title:     "scn3 Disk Sunburst",
		Width:     settings.Width,
		Height:    settings.Height,
		MinWidth:  640,
		MinHeight: 480,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup: func(ctx context.Context) {
			dlg.ctx = ctx
			app.SetEmit(func(event string, data any) { runtime.EventsEmit(ctx, event, data) })
		},
		OnBeforeClose: func(ctx context.Context) bool {
			s := desktop.LoadSettings()
			s.Width, s.Height = runtime.WindowGetSize(ctx)
			// Failing to remember the window size is no reason to block closing.
			_ = desktop.SaveSettings(s)
			return false
		},
		OnShutdown: func(context.Context) { app.Shutdown() },
		Bind:       []any{app, dlg},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "scn3:", err)
		os.Exit(1)
	}
}
