package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/prism/internal/appicon"
	"github.com/nomad-software/prism/internal/desktop"
	"github.com/nomad-software/prism/internal/instance"
	"github.com/nomad-software/prism/internal/settings"
	"github.com/nomad-software/prism/internal/stats"
	"github.com/nomad-software/prism/internal/ui"
)

func main() {
	// One copy runs at a time: one started while another runs, as from the
	// desktop, has that one's window shown and leaves, passing on the
	// desktop's leave to take the focus. Nothing else is done before, so
	// that it leaves at once.
	var claim *instance.Instance
	if dir, err := instance.Dir(); err == nil {
		claim, err = instance.Claim(dir, os.Getenv("XDG_ACTIVATION_TOKEN"))
		switch {
		case errors.Is(err, instance.ErrRunning):
			return
		case errors.Is(err, instance.ErrNoAnswer):
			fmt.Fprintln(os.Stderr, "prism:", err)
			os.Exit(1)
		case err != nil:
			// The program runs, as it would without the check.
			fmt.Fprintln(os.Stderr, "prism: another copy could be started beside this one:", err)
		default:
			defer claim.Close()
		}
	} else if !errors.Is(err, errors.ErrUnsupported) {
		fmt.Fprintln(os.Stderr, "prism: another copy could be started beside this one:", err)
	}

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
	// matches to the desktop entry of that name for its icon and its place
	// in the task bar.
	qt.QGuiApplication_SetDesktopFileName(desktop.Name)

	// On Linux the desktop is given an entry for the program, which starts
	// the copy that is running now, wherever it is and whatever it is
	// called, so that it can be started from the desktop's own menus and
	// kept in a dock or sidebar.
	if runtime.GOOS == "linux" {
		if err := desktop.Install(appicon.PNG); err != nil {
			fmt.Fprintln(os.Stderr, "prism: the desktop entry could not be written:", err)
		}
	}

	window := ui.NewWindow(path, stats.New())
	window.Start()
	if claim != nil {
		claim.Serve(func(token string) {
			mainthread.Start(func() { window.Activate(token) })
		})
	}

	qt.QApplication_Exec()
}
