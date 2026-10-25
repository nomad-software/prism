package main

import (
	"fmt"
	"os"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/prism/internal/settings"
	"github.com/nomad-software/prism/internal/stats"
	"github.com/nomad-software/prism/internal/ui"
)

func main() {
	// The settings are kept where the system keeps the configuration of
	// programs, or in the current directory if it has no such place.
	path, err := settings.Prepared()
	if err != nil {
		fmt.Fprintln(os.Stderr, "prism: the settings are kept in the current directory:", err)
		path = "settings.json"
	}

	qt.NewQApplication(os.Args)
	qt.QCoreApplication_SetApplicationName("prism")
	// Under Wayland this is the name a window goes by, which a desktop
	// matches to the desktop entry of that name for its icon.
	qt.QGuiApplication_SetDesktopFileName("prism")

	window := ui.NewWindow(path, stats.New())
	window.Start()
	qt.QApplication_Exec()
}
