package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app, err := NewApp()
	if err != nil {
		log.Fatalf("create Agent X: %v", err)
	}

	err = wails.Run(&options.App{
		Title:            "Agent X",
		Width:            1280,
		Height:           820,
		MinWidth:         940,
		MinHeight:        640,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 245, G: 245, B: 245, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
		Mac: &mac.Options{
			TitleBar:   mac.TitleBarHiddenInset(),
			Appearance: mac.NSAppearanceNameAqua,
		},
		Windows: &windows.Options{
			Theme: windows.SystemDefault,
		},
	})
	if err != nil {
		log.Fatalf("run Agent X: %v", err)
	}
}
