# Prism

A desktop system monitor in Go with Qt 6 through the miqt bindings, modelled on ApeMail (`../apemail`) and on the Resources tab of GNOME's System Monitor. Linux is the primary OS; macOS is supported.

It graphs CPU use (a line per core, or one for all), memory and swap, and network traffic (a line per adapter, Download and Upload graphs side by side). Graphs and legends are painted on plain QWidgets. The File menu has Settings and Quit, and a status bar along the bottom has a size grip to drag.

## Rules

* Build the app after every change: `make build`, and `make vet` and `make test` when code changed.
    * Run them in the foreground, one at a time. The tests run without `-race`: the app runs on the Qt main thread, and a race build of miqt costs minutes.
    * Lint with the installed `staticcheck ./...` and `deadcode -test ./...`; both must be clean.
    * CI (`.github/workflows/build.yml`) also builds, vets and tests on every push and pull request.
* Keep this file and README.md up to date with every change.
* Dependencies come from their proper upstream remotes only.
    * Never use local dependencies or `replace` directives in go.mod, not even to point at a fork.
    * miqt is `github.com/mappu/miqt`; "the latest" means its newest tag, or master when asked.
* Statistics come only from /proc and /sys on Linux, and the system's own interfaces (Mach, sysctl) on macOS.
* Don't commit or create the git repository; the user does that.
* Delete only what `qt.New…` makes. A value miqt returns by value (`Position()`, `QColorDialog_GetColor4`, `Font()` and the like) is freed by its finalizer, so deleting it too is a double free.
* Match ApeMail's style: plain prose comments, `mainthread.Wait` around Qt calls in tests, settings via `os.UserConfigDir()`.

## Behaviour

* Settings are kept in `prism/settings.json` under `os.UserConfigDir()`, as ApeMail's are, with the window size.
* Settings tabs, laid out as TriggerAPI's (`../triggerapi`) are, with 20px tab margins and bold section headings:
    * General: an update interval per resource (whole seconds, 1 to 3600, default 2); CPU mode (each core or all); units: sizes binary (KiB, default) or decimal (kB), and network speed in bytes (default) or bits a second.
    * Network: a table of every adapter seen, ticked if shown, with an editable alias; unticked ones are kept in `HiddenAdapters` and left out of the network graphs, legend and scale, but still read. Aliases are kept in `AdapterAliases` and shown in the legend in place of the adapter's name.
    * User interface: an Appearance section with the theme (light, dark, follow the system, as ApeMail), then a Colours section: a TriggerAPI-style Name/Colour table with bold section rows (Graphs, CPU, Network) and filled 140px swatch buttons naming the colour; "Use Default Colours" resets only colours.
    * Restore Defaults (button box) resets every setting.
* Every graph is drawn across its whole width from the first reading: CPU and network lines at 0 until the second, memory at its first reading.
* The memory graph plots bytes in use (memory and swap) against a scale from 0 to the installed memory (raised if swap use is higher), labelled in the size units; the legend says "4.0 GiB (25.0%)", without the total.
* Default colours are Solarized's eight accents (blue, orange, cyan, magenta, green, violet, yellow, red), readable on light and dark, on a grid of Ambiance warm grey #aea79f at 40%: all cores blue, memory blue, swap red; cores take green, orange, cyan, magenta, yellow, violet, blue, red; adapters take blue, yellow, magenta, cyan, orange, violet, green, red. These are the user's own choices. Beyond eight, variants turn the hue 10° more each round, alternating direction, adjusted to at least 2.2:1 contrast on #ffffff and #1e1e1e. The Colours table lists every core the CPU reading found (no cap). Saved colours of cores beyond those listed are kept, as unlisted adapters' are.
* A graph spans 60 seconds, or two update intervals when the interval is longer than 30 seconds, so a long interval still draws a line.
* Every network adapter is read, `lo` and virtual ones included, and shown unless hidden in the settings. Each gets a colour when first seen, which is saved straight away and kept for when it reappears.
* Legends use coloured swatches, which are clickable (pointing-hand cursor and a ring in the text colour just inside the swatch on hover) to choose that line's colour, applied and saved at once as the settings dialog would; the CPU legend wraps into even columns and lists the cores from the first reading, with "—" until their use is known. Every legend is one line per entry, the value beside the name: CPU values right-aligned at full opacity; memory, swap and network values left-aligned at 40% opacity. A network entry reads "↓ 81.2 kb/s (7.4 MB)   ↑ 23.3 kb/s (1.0 MB)", rates with totals in brackets. Every legend name is in the regular font, none bold. The network graphs have no titles; each has "Download" or "Upload" written in the top left of its grid, in the scale labels' font, 10px in from the top and left, in the text colour at 20% opacity. Graph scale and time labels are drawn whole at 75% of the window font (at least 7pt) and 50% opacity. There is no memory cache line and no folding arrows on section titles.
* Module `github.com/nomad-software/prism`, binary `prism`, display name "Prism". The icon, drawn in Go (`make icons`), is a prism: an equilateral triangle on a dark rounded tile, divided from its middle to the middle of each side into red (top), green (bottom left) and blue (bottom right) thirds with thin gaps between.
* No desktop-entry install or single-instance handling, unlike ApeMail.

## Layout

* `internal/stats`: reads counters; /proc and /sys on Linux, Mach and sysctl via cgo on macOS (`system_darwin.go`, only built and tested by the macOS workflow).
* `internal/usage`: CPU busy % and network rates between readings.
* `internal/history`: the points of each graph line.
* `internal/chart`: scales, labels, point placement and legend layout (pure, no Qt).
* `internal/settings`: load/save, defaults and colours.
* `internal/appicon`: the icon and its drawing.
* `internal/ui`: the window, painted graphs and legends, settings dialog, themes.
* `packaging/` and `.github/workflows/`: AppImage and macOS app (`appimage.yml`, `macos.yml`, run on `v` tags or by hand), as ApeMail does, plus `build.yml`.
