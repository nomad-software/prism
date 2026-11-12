// Package ui is the program's window: graphs of the use of the processors,
// memory and swap, and network adapters, each read at an interval of its
// own, a table of the processes, and the settings of how they are read and
// drawn.
package ui

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"time"

	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"

	"github.com/nomad-software/prism/internal/appicon"
	"github.com/nomad-software/prism/internal/chart"
	"github.com/nomad-software/prism/internal/history"
	"github.com/nomad-software/prism/internal/settings"
	"github.com/nomad-software/prism/internal/stats"
	"github.com/nomad-software/prism/internal/usage"
)

// Window is the program's main window.
type Window struct {
	qtwidgets.QMainWindowBase
	// path is where the settings are saved.
	path   string
	config settings.Settings
	source stats.Source
	// now is the time, which tests set.
	now func() time.Time
	// file and help are the File and Help menus.
	file, help *qtwidgets.QMenu
	// tabs has a tab of the graphs, and one of the processes, and tabKeys
	// are the shortcuts to each, in order.
	tabs    *tabWidget
	tabKeys []*qtgui.QShortcut

	cpu struct {
		section
		usage usage.CPU
		all   history.Series
		cores []history.Series
		graph *graph
		// legend is for a line for each processor, and total for one of
		// all of them; only that of the mode the settings choose is shown.
		legend, total *legend
		// measured says whether the use of the processors is known, which
		// it is from the second reading.
		measured bool
		// processors is how many processors were online at the last
		// reading, which no process can use more of.
		processors int
	}
	memory struct {
		section
		memory, swap history.Series
		last         stats.Memory
		graph        *graph
		legend       *legend
	}
	network struct {
		section
		usage          usage.Network
		adapters       map[string]*adapter
		received, sent *graph
		legend         *legend
	}
	processes processes
	// frames draws the graphs again between readings, as often as their
	// lines move along by a pixel, so that they move smoothly.
	frames *qtcore.QTimer
}

// section is the part of the window of one statistic: its heading, where
// what went wrong reading it is said, and the timer it is read by.
type section struct {
	*qtwidgets.QWidget
	layout *qtwidgets.QVBoxLayout
	errors *qtwidgets.QLabel
	timer  *qtcore.QTimer
}

// adapter is what is kept of a network adapter.
type adapter struct {
	received, sent history.Series
	rate           usage.Rate
	// present says whether it was in the last reading. One that is not is
	// kept until its lines have gone off the left of the graphs.
	present bool
}

// NewWindow makes the window, with the settings kept at the path, reading the
// statistics from the source. Start shows it.
func NewWindow(path string, source stats.Source) *Window {
	w := &Window{
		path:   path,
		config: settings.Load(path),
		source: source,
		now:    time.Now,
	}
	qtwidgets.NewQMainWindowWith(w, nil)
	if w.config.Theme != "" {
		applyTheme(w.config.Theme)
	}
	w.SetWindowTitle("Prism")
	icon := appIcon()
	w.SetWindowIcon(icon)
	// Dialogs take their icon from the application.
	qtgui.QGuiApplicationSetWindowIcon(icon)
	w.Resize(w.config.Width, w.config.Height)
	w.makeMenus()

	resources, layout := newTabPage()
	layout.SetSpacing(18)
	w.makeCPU()
	w.makeMemory()
	w.makeNetwork()
	layout.AddWidgetFull(w.cpu.QWidget, 1, 0)
	layout.AddWidgetFull(w.memory.QWidget, 1, 0)
	layout.AddWidgetFull(w.network.QWidget, 1, 0)
	processes, layout := newTabPage()
	w.makeProcesses()
	layout.AddWidget(w.processes.QWidget)
	w.makeTabs(resources, processes)
	w.SetCentralWidget(w.tabs)
	// A status bar along the bottom, with a handle in its corner to drag
	// the window bigger or smaller by.
	w.StatusBar().SetSizeGripEnabled(true)

	w.cpu.timer = w.newTimer(w.config.Intervals.CPU, w.sampleCPU)
	w.memory.timer = w.newTimer(w.config.Intervals.Memory, w.sampleMemory)
	w.network.timer = w.newTimer(w.config.Intervals.Network, w.sampleNetwork)
	w.processes.timer = w.newTimer(w.config.Intervals.Processes, w.sampleProcesses)
	w.frames = qtcore.NewQTimer(w)
	// A frame is a few milliseconds, which a coarse timer is out by.
	w.frames.SetTimerType(qtcore.PreciseTimer)
	w.frames.OnTimeout(w.animate)
	return w
}

// CloseEvent saves the settings, with the size of the window, as it closes.
func (w *Window) CloseEvent(event *qtgui.QCloseEvent) {
	w.saveSettings()
	w.Super().CloseEvent(event)
}

// newTabPage returns the page of a tab of the window, and its layout, with the
// margins every page has.
func newTabPage() (*qtwidgets.QWidget, *qtwidgets.QVBoxLayout) {
	page := qtwidgets.NewQWidget(nil)
	layout := qtwidgets.NewQVBoxLayout(page)
	layout.SetContentsMargins(qtcore.QMargins{Left: 32, Top: 12, Right: 32, Bottom: 32})
	return page, layout
}

// Start shows the window, takes the first reading of each statistic and of
// the processes, and starts reading them at their intervals. Every graph is
// drawn across from the first reading; the use of the processors and
// processes and the rates of the network adapters are worked out from one
// reading to the next, so they are 0, or not known, until the second.
func (w *Window) Start() {
	if w.config.Maximized {
		w.ShowMaximized()
	} else {
		w.Show()
	}
	w.sampleCPU()
	w.sampleMemory()
	w.sampleNetwork()
	w.sampleProcesses()
	for _, t := range []*qtcore.QTimer{w.cpu.timer, w.memory.timer, w.network.timer, w.processes.timer} {
		t.Start()
	}
	w.frames.SetInterval(w.frameInterval())
	w.frames.Start()
}

// Activate shows the window, raised and given the focus, as when the program is
// started again while it runs. token is the desktop's leave to take the focus
// under Wayland, which is refused without it; Qt reads it from the
// environment when it asks for the focus, and then takes it out.
func (w *Window) Activate(token string) {
	if token != "" {
		os.Setenv("XDG_ACTIVATION_TOKEN", token)
	}
	w.SetWindowState(w.WindowState() &^ qtcore.WindowMinimized)
	w.Show()
	w.Raise()
	w.ActivateWindow()
}

// newTimer returns a timer, not yet started, that calls read at the interval,
// in seconds.
func (w *Window) newTimer(interval float64, read func()) *qtcore.QTimer {
	t := qtcore.NewQTimer(w)
	t.SetInterval(timerInterval(interval))
	t.OnTimeout(read)
	return t
}

// timerInterval returns the interval, in seconds, as a timer has it, in
// milliseconds.
func timerInterval(interval float64) int {
	return int(settings.Duration(interval) / time.Millisecond)
}

// The time between frames is no less than minFrame, 60 frames a second, and
// no more than maxFrame, so that a graph made wider or given a shorter span
// soon moves as often as it then needs to.
const (
	minFrame = time.Second / 60
	maxFrame = time.Second
)

// graphs returns the graphs of the window.
func (w *Window) graphs() []*graph {
	return []*graph{w.cpu.graph, w.memory.graph, w.network.received, w.network.sent}
}

// frameInterval returns how often the graphs are moved along, in
// milliseconds: as often as the fastest of their lines moves by a pixel.
func (w *Window) frameInterval() int {
	interval := maxFrame
	for _, g := range w.graphs() {
		interval = min(interval, g.pixelTime())
	}
	return int(max(interval, minFrame) / time.Millisecond)
}

// animate moves the graphs on to now, each drawn again if it is shown and its
// lines have moved by a pixel. Their lines hold their newest values to now
// until the next reading puts them right. Nothing is drawn while the window
// is minimized.
func (w *Window) animate() {
	if ms := w.frameInterval(); w.frames.Interval() != ms {
		w.frames.SetInterval(ms)
	}
	if w.IsMinimized() {
		return
	}
	now := w.now()
	for _, g := range w.graphs() {
		g.advance(now)
	}
}

// makeMenus makes the menu bar: a File menu and a Help menu.
func (w *Window) makeMenus() {
	w.file = w.MenuBar().AddMenuTitle("&File")
	settingsAction := w.file.AddActionText("&Settings…")
	setShortcut(settingsAction, "Ctrl+,")
	settingsAction.OnTriggered(func(bool) { w.editSettings() })
	w.file.AddSeparator()
	quit := w.file.AddActionText("&Quit")
	setShortcut(quit, "Ctrl+Q")
	quit.OnTriggered(func(bool) { w.Close() })

	w.help = w.MenuBar().AddMenuTitle("&Help")
	about := w.help.AddActionText("&About")
	setShortcut(about, "Ctrl+I")
	about.OnTriggered(func(bool) { showAbout(w) })
}

// setShortcut gives the action the shortcut.
func setShortcut(action *qtgui.QAction, keys string) {
	action.SetShortcut(qtgui.NewQKeySequenceWithKey(keys))
}

// newSection makes the part of the window with the heading.
func newSection(title string) section {
	s := section{QWidget: qtwidgets.NewQWidget(nil)}
	s.layout = qtwidgets.NewQVBoxLayout(s.QWidget)
	s.layout.SetContentsMargins(qtcore.QMargins{Left: 0, Top: 0, Right: 0, Bottom: 0})
	s.layout.SetSpacing(6)

	heading := qtwidgets.NewQHBoxLayout(nil)
	heading.AddWidget(sectionHeading(title))
	s.errors = qtwidgets.NewQLabel(nil)
	s.errors.SetVisible(false)
	heading.AddWidgetFull(s.errors, 1, 0)
	s.layout.AddLayout(heading)
	return s
}

// report says what went wrong reading the statistic of the section, or
// clears what was said with nil.
func (s *section) report(err error) {
	if err == nil {
		s.errors.SetVisible(false)
		return
	}
	s.errors.SetText("— " + err.Error())
	s.errors.SetVisible(true)
}

// makeCPU makes the section of the processors: their graph, and a legend
// for each mode they can be shown in.
func (w *Window) makeCPU() {
	w.cpu.section = newSection("CPU")
	w.cpu.graph = newGraph()
	w.cpu.legend = newLegend(false)
	w.cpu.total = newLegend(false)
	w.cpu.layout.AddWidgetFull(w.cpu.graph, 1, 0)
	w.cpu.layout.AddWidget(w.cpu.legend)
	w.cpu.layout.AddWidget(w.cpu.total)
}

// makeMemory makes the section of memory and swap.
func (w *Window) makeMemory() {
	w.memory.section = newSection("Memory and Swap")
	w.memory.graph = newGraph()
	w.memory.legend = newLegend(true)
	w.memory.layout.AddWidgetFull(w.memory.graph, 1, 0)
	w.memory.layout.AddWidget(w.memory.legend)
}

// makeNetwork makes the section of the network adapters: a graph of what
// they receive, titled Download, beside one of what they send, titled
// Upload.
func (w *Window) makeNetwork() {
	w.network.section = newSection("Network")
	w.network.adapters = map[string]*adapter{}
	w.network.received = newGraph()
	w.network.received.title = "Download"
	w.network.sent = newGraph()
	w.network.sent.title = "Upload"
	w.network.legend = newLegend(true)

	graphs := qtwidgets.NewQHBoxLayout(nil)
	graphs.SetSpacing(16)
	graphs.AddWidgetFull(w.network.received, 1, 0)
	graphs.AddWidgetFull(w.network.sent, 1, 0)
	w.network.layout.AddLayoutFull(graphs, 1)
	w.network.layout.AddWidget(w.network.legend)
}

// span returns the time a graph read at the interval, in seconds, spans.
func span(interval float64) time.Duration {
	return history.Span(settings.Duration(interval))
}

// sampleCPU reads the times of the processors, and shows how busy they were
// since they were last read. Until the second reading, and for a processor
// until its own second, the lines are 0, drawn across the whole graph from
// the first.
func (w *Window) sampleCPU() {
	c, err := w.source.CPU()
	w.cpu.report(err)
	if err != nil {
		return
	}
	// The processors are listed from the first reading, though their use
	// is only known from the second.
	for len(w.cpu.cores) < len(c.Cores) {
		w.cpu.cores = append(w.cpu.cores, history.Series{})
	}
	if n := c.Processors(); n != w.cpu.processors {
		w.cpu.processors = n
		w.processes.nameProcessors(n)
	}
	all, cores, ok := w.cpu.usage.Update(c)
	if ok {
		w.cpu.measured = true
	}
	now := w.now()
	s := span(w.config.Intervals.CPU)
	w.cpu.all.Record(now, s, 0, all)
	for i := range w.cpu.cores {
		// A processor that is no longer read, as one taken offline, is
		// idle.
		busy := 0.0
		if i < len(cores) {
			busy = cores[i]
		}
		w.cpu.cores[i].Record(now, s, 0, busy)
	}
	w.showCPU()
}

// showCPU draws the graph and legend of the processors, as the settings
// say: a line for each, or one for all of them.
func (w *Window) showCPU() {
	colours := w.config.Colours
	total := w.config.CPUMode == settings.CPUTotal
	var lines []line
	var entries []entry
	if total {
		lines = append(lines, line{colours.Total, w.cpu.all.Points(), true})
		entries = append(entries, entry{colours.Total, "CPU", w.cpuPercent(&w.cpu.all), w.colourSetter(func(c *settings.Colours, colour string) {
			c.Total = colour
		})})
	} else {
		for i := range w.cpu.cores {
			s := &w.cpu.cores[i]
			lines = append(lines, line{colours.Core(i), s.Points(), true})
			entries = append(entries, entry{colours.Core(i), fmt.Sprintf("CPU%d", i+1), w.cpuPercent(s), w.colourSetter(func(c *settings.Colours, colour string) {
				// A core set to its default is not kept as a choice, as
				// the settings dialog has it.
				if colour == settings.DefaultCore(i) {
					colour = ""
				}
				c.SetCore(i, colour)
			})})
		}
	}
	w.cpu.graph.show(w.now(), span(w.config.Intervals.CPU), 100, chart.PercentLabels(), lines, colours.Grid)
	w.cpu.legend.SetVisible(!total)
	w.cpu.total.SetVisible(total)
	if total {
		w.cpu.total.show(entries)
	} else {
		w.cpu.legend.show(entries)
	}
}

// cpuPercent writes the newest use of a processor, or "—" while it is not
// yet known.
func (w *Window) cpuPercent(s *history.Series) string {
	if !w.cpu.measured {
		return "—"
	}
	p, _ := s.Last()
	return chart.FormatPercent(p.V)
}

// sampleMemory reads the use of memory and swap, and shows it.
func (w *Window) sampleMemory() {
	m, err := w.source.Memory()
	w.memory.report(err)
	if err != nil {
		return
	}
	now := w.now()
	w.memory.last = m
	s := span(w.config.Intervals.Memory)
	w.memory.memory.Record(now, s, float64(m.Used), float64(m.Used))
	w.memory.swap.Record(now, s, float64(m.SwapUsed), float64(m.SwapUsed))
	w.showMemory()
}

// percent returns the percentage of the whole that the part is, or 0 if
// there is no whole.
func percent(part, whole uint64) float64 {
	if whole == 0 {
		return 0
	}
	return 100 * float64(part) / float64(whole)
}

// showMemory draws the graph and legend of memory and swap.
func (w *Window) showMemory() {
	colours := w.config.Colours
	m := w.memory.last
	lines := []line{
		{colours.Memory, w.memory.memory.Points(), true},
		{colours.Swap, w.memory.swap.Points(), true},
	}
	// The graph is of how much is in use, up to the maximum chosen or else
	// the power of 2 the memory comes to, in the units of sizes, or to more
	// than that if it is in use.
	s := span(w.config.Intervals.Memory)
	since := w.now().Add(-s)
	most := w.config.MemoryMax
	if most == 0 {
		most = chart.DefaultMemoryMax(m.Total)
	}
	u := w.units()
	top := max(float64(most)*u.Gigabyte(), w.memory.memory.Max(since), w.memory.swap.Max(since))
	w.memory.graph.show(w.now(), s, top, u.SizeLabels(top), lines, colours.Grid)
	w.memory.legend.show([]entry{
		{colours.Memory, "Memory", usedOf(u, m.Used, m.Total), w.colourSetter(func(c *settings.Colours, colour string) {
			c.Memory = colour
		})},
		{colours.Swap, "Swap", usedOf(u, m.SwapUsed, m.SwapTotal), w.colourSetter(func(c *settings.Colours, colour string) {
			c.Swap = colour
		})},
	})
}

// colourSetter returns what sets a colour of the settings, by the function
// that puts it in the colours, and applies and saves them, as the settings
// dialog would.
func (w *Window) colourSetter(set func(c *settings.Colours, colour string)) func(string) {
	return func(colour string) {
		s := w.config
		s.Colours = s.Colours.Clone()
		set(&s.Colours, colour)
		w.applySettings(s)
	}
}

// usedOf says how much of the whole is used: "14.2 GiB (48.3%)".
func usedOf(u chart.Units, used, total uint64) string {
	if total == 0 {
		return "Not available"
	}
	return fmt.Sprintf("%s (%s)", u.Size(float64(used)), chart.FormatPercent(percent(used, total)))
}

// units returns the units the settings choose to write sizes and rates in.
func (w *Window) units() chart.Units {
	return chart.Units{
		Decimal: w.config.SizeUnits == settings.SizeDecimal,
		Bits:    w.config.RateUnits == settings.RateBits,
	}
}

// sampleNetwork reads the counters of the network adapters, and shows how
// fast each received and sent since they were last read. An adapter seen for
// the first time is given a colour, which the settings keep.
func (w *Window) sampleNetwork() {
	counters, err := w.source.Network()
	w.network.report(err)
	if err != nil {
		return
	}
	now := w.now()
	s := span(w.config.Intervals.Network)
	added := false
	for _, a := range w.network.adapters {
		a.present = false
	}
	rates := w.network.usage.Update(now, counters)
	// Adapters seen for the first time together are given colours in the
	// order of their names, so that which gets which is the same each time.
	for _, name := range sortedKeys(rates) {
		rate := rates[name]
		a := w.network.adapters[name]
		if a == nil {
			a = &adapter{}
			w.network.adapters[name] = a
		}
		a.present, a.rate = true, rate
		// Until an adapter's second reading its lines are 0 across the
		// graphs.
		a.received.Record(now, s, 0, rate.Received)
		a.sent.Record(now, s, 0, rate.Sent)
		if _, given := w.config.Colours.Adapter(name); given {
			added = true
		}
	}
	before := now.Add(-s)
	for name, a := range w.network.adapters {
		if a.present {
			continue
		}
		a.received.Trim(before)
		a.sent.Trim(before)
		// An adapter that has gone is forgotten once its lines have gone
		// off the left of the graphs, which a line of one point has.
		if last, ok := a.received.Last(); !ok || last.T.Before(before) {
			delete(w.network.adapters, name)
		}
	}
	if added {
		w.saveConfig()
	}
	w.showNetwork()
}

// showNetwork draws the graphs of what the network adapters received and
// sent, each to the scale of its largest rate, and their legend, of those
// that are present, by name. Adapters the settings hide are left out of
// both; they are still read, so that their lines are whole when they are
// shown again.
func (w *Window) showNetwork() {
	now := w.now()
	s := span(w.config.Intervals.Network)
	since := now.Add(-s)
	var received, sent []line
	var largestReceived, largestSent float64
	var entries []entry
	u := w.units()
	for _, name := range sortedKeys(w.network.adapters) {
		if !w.config.Shown(name) {
			continue
		}
		a := w.network.adapters[name]
		colour, _ := w.config.Colours.Adapter(name)
		// An adapter that has gone is not held, and its lines end at its
		// last reading.
		received = append(received, line{colour, a.received.Points(), a.present})
		sent = append(sent, line{colour, a.sent.Points(), a.present})
		largestReceived = max(largestReceived, a.received.Max(since))
		largestSent = max(largestSent, a.sent.Max(since))
		if a.present {
			entries = append(entries, entry{colour, w.config.AdapterLabel(name), fmt.Sprintf("↓ %s (%s)   ↑ %s (%s)",
				u.Rate(a.rate.Received), u.Size(float64(a.rate.Total.Received)),
				u.Rate(a.rate.Sent), u.Size(float64(a.rate.Total.Sent))),
				w.colourSetter(func(c *settings.Colours, colour string) { c.Network[name] = colour })})
		}
	}
	grid := w.config.Colours.Grid
	top := u.RateTop(largestReceived)
	w.network.received.show(now, s, top, u.RateLabels(top), received, grid)
	top = u.RateTop(largestSent)
	w.network.sent.show(now, s, top, u.RateLabels(top), sent, grid)
	w.network.legend.show(entries)
}

// sortedKeys returns the keys of the map, in order.
func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}

// editSettings shows the settings, and puts them into effect if they are
// changed.
func (w *Window) editSettings() *settingsDialog {
	return newSettingsDialog(w, w.config, len(w.cpu.cores), sortedKeys(w.config.Colours.Network), chart.DefaultMemoryMax(w.memory.last.Total), w.applySettings)
}

// applySettings puts the settings into effect, and saves them.
func (w *Window) applySettings(s settings.Settings) {
	if s.Theme != w.config.Theme {
		applyTheme(s.Theme)
	}
	// The size of the window is what it is now, not what it was when the
	// settings were shown.
	s.Width, s.Height, s.Maximized = w.config.Width, w.config.Height, w.config.Maximized
	w.config = s
	for _, t := range []struct {
		timer    *qtcore.QTimer
		interval float64
	}{
		{w.cpu.timer, s.Intervals.CPU},
		{w.memory.timer, s.Intervals.Memory},
		{w.network.timer, s.Intervals.Network},
		{w.processes.timer, s.Intervals.Processes},
	} {
		if ms := timerInterval(t.interval); t.timer.Interval() != ms {
			t.timer.SetInterval(ms)
		}
	}
	w.showCPU()
	w.showMemory()
	w.showNetwork()
	w.showProcesses()
	w.saveSettings()
}

// saveSettings saves the settings, with the size of the window.
func (w *Window) saveSettings() {
	w.config.Maximized = w.IsMaximized()
	if !w.config.Maximized {
		w.config.Width, w.config.Height = w.Width(), w.Height()
	}
	w.saveConfig()
}

// saveConfig saves the settings as they are.
func (w *Window) saveConfig() {
	if err := w.config.Save(w.path); err != nil {
		fmt.Fprintln(os.Stderr, "prism: the settings could not be saved:", err)
	}
}

// appIcon returns the icon of the program.
func appIcon() *qtgui.QIcon {
	pixmap := qtgui.NewQPixmap()
	pixmap.LoadFromData(appicon.PNG)
	// The icon keeps a copy of the picture.
	return qtgui.NewQIconWithPixmap(pixmap)
}
