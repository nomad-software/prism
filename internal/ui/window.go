// Package ui is the program's window: graphs of the use of the processors,
// memory and swap, and network adapters, each read at an interval of its
// own, a table of the processes, and the settings of how they are read and
// drawn.
package ui

import (
	"fmt"
	"maps"
	"math"
	"os"
	"slices"
	"time"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/prism/internal/appicon"
	"github.com/nomad-software/prism/internal/chart"
	"github.com/nomad-software/prism/internal/history"
	"github.com/nomad-software/prism/internal/settings"
	"github.com/nomad-software/prism/internal/stats"
	"github.com/nomad-software/prism/internal/usage"
)

// Window is the program's main window.
type Window struct {
	*qt.QMainWindow
	// path is where the settings are saved.
	path   string
	config settings.Settings
	source stats.Source
	// now is the time, which tests set.
	now func() time.Time
	// file and help are the File and Help menus.
	file, help *qt.QMenu
	// tabs has a tab of the graphs, and one of the processes, and tabKeys
	// are the shortcuts to each, in order.
	tabs    *qt.QTabWidget
	tabKeys []*qt.QShortcut

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
	// frames draws the graphs again between readings, at the frame rate
	// the settings choose, so that their lines move along smoothly.
	frames *qt.QTimer
	// started says that Start has begun reading the statistics, after
	// which the graphs are animated.
	started bool
}

// section is the part of the window of one statistic: its heading, where
// what went wrong reading it is said, and the timer it is read by.
type section struct {
	*qt.QWidget
	layout *qt.QVBoxLayout
	errors *qt.QLabel
	timer  *qt.QTimer
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
		QMainWindow: qt.NewQMainWindow2(),
		path:        path,
		config:      settings.Load(path),
		source:      source,
		now:         time.Now,
	}
	rememberSystemStyle()
	if w.config.Theme != "" {
		applyTheme(w.config.Theme)
	}
	w.SetWindowTitle("Prism")
	icon := appIcon()
	defer icon.Delete()
	w.SetWindowIcon(icon)
	// Dialogs take their icon from the application.
	qt.QGuiApplication_SetWindowIcon(icon)
	w.Resize(w.config.Width, w.config.Height)
	w.makeMenus()

	resources := qt.NewQWidget2()
	layout := qt.NewQVBoxLayout(resources)
	layout.SetContentsMargins(32, 12, 32, 32)
	layout.SetSpacing(18)
	w.makeCPU()
	w.makeMemory()
	w.makeNetwork()
	layout.AddWidget2(w.cpu.QWidget, 1)
	layout.AddWidget2(w.memory.QWidget, 1)
	layout.AddWidget2(w.network.QWidget, 1)
	processes := qt.NewQWidget2()
	layout = qt.NewQVBoxLayout(processes)
	layout.SetContentsMargins(32, 12, 32, 32)
	w.makeProcesses()
	layout.AddWidget(w.processes.QWidget)
	w.makeTabs(resources, processes)
	w.SetCentralWidget(w.tabs.QWidget)
	// A status bar along the bottom, with a handle in its corner to drag
	// the window bigger or smaller by.
	w.StatusBar().SetSizeGripEnabled(true)

	w.cpu.timer = w.newTimer(w.config.Intervals.CPU, w.sampleCPU)
	w.memory.timer = w.newTimer(w.config.Intervals.Memory, w.sampleMemory)
	w.network.timer = w.newTimer(w.config.Intervals.Network, w.sampleNetwork)
	w.processes.timer = w.newTimer(w.config.Intervals.Processes, w.sampleProcesses)
	w.frames = qt.NewQTimer2(w.QObject)
	// A frame is a few milliseconds, which a coarse timer is out by.
	w.frames.SetTimerType(qt.PreciseTimer)
	w.frames.OnTimeout(w.animate)

	w.OnCloseEvent(func(super func(*qt.QCloseEvent), event *qt.QCloseEvent) {
		w.saveSettings()
		super(event)
	})
	return w
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
	for _, t := range []*qt.QTimer{w.cpu.timer, w.memory.timer, w.network.timer, w.processes.timer} {
		t.Start2()
	}
	w.started = true
	w.setFrameRate(w.config.FrameRate)
}

// Activate shows the window, raised and given the focus, as when the program is
// started again while it runs. token is the desktop's leave to take the focus
// under Wayland, which is refused without it; Qt reads it from the
// environment when it asks for the focus, and then takes it out.
func (w *Window) Activate(token string) {
	if token != "" {
		os.Setenv("XDG_ACTIVATION_TOKEN", token)
	}
	w.SetWindowState(w.WindowState() &^ qt.WindowMinimized)
	w.Show()
	w.Raise()
	w.ActivateWindow()
}

// newTimer returns a timer, not yet started, that calls read at the interval,
// in seconds.
func (w *Window) newTimer(interval float64, read func()) *qt.QTimer {
	t := qt.NewQTimer2(w.QObject)
	t.SetInterval(int(settings.Duration(interval) / time.Millisecond))
	t.OnTimeout(read)
	return t
}

// setFrameRate draws the graphs the number of times a second between
// readings, or only at each reading where it is 0.
func (w *Window) setFrameRate(fps int) {
	if fps <= 0 {
		w.frames.Stop()
		return
	}
	if ms := int(math.Round(1000 / float64(fps))); w.frames.Interval() != ms {
		w.frames.SetInterval(ms)
	}
	if !w.frames.IsActive() {
		w.frames.Start2()
	}
}

// animate moves the graphs on to now. Their lines hold their newest values
// to now until the next reading puts them right. A graph that is not shown is
// not drawn.
func (w *Window) animate() {
	now := w.now()
	for _, g := range []*graph{w.cpu.graph, w.memory.graph, w.network.received, w.network.sent} {
		g.advance(now)
	}
}

// makeMenus makes the menu bar: a File menu and a Help menu. On macOS the
// roles of their actions move them into the application's menu.
func (w *Window) makeMenus() {
	w.file = w.MenuBar().AddMenuWithTitle("&File")
	settingsAction := w.file.AddActionWithText("&Settings…")
	settingsAction.SetMenuRole(qt.QAction__PreferencesRole)
	shortcut := qt.NewQKeySequence2("Ctrl+,")
	settingsAction.SetShortcut(shortcut)
	shortcut.Delete()
	settingsAction.OnTriggered(func() { w.editSettings() })
	w.file.AddSeparator()
	quit := w.file.AddActionWithText("&Quit")
	shortcut = qt.NewQKeySequence2("Ctrl+Q")
	quit.SetShortcut(shortcut)
	shortcut.Delete()
	quit.SetMenuRole(qt.QAction__QuitRole)
	quit.OnTriggered(func() { w.Close() })

	w.help = w.MenuBar().AddMenuWithTitle("&Help")
	about := w.help.AddActionWithText("&About")
	about.SetMenuRole(qt.QAction__AboutRole)
	shortcut = qt.NewQKeySequence2("Ctrl+I")
	about.SetShortcut(shortcut)
	shortcut.Delete()
	about.OnTriggered(func() { showAbout(w.QWidget) })
}

// newSection makes the part of the window with the heading.
func newSection(title string) section {
	s := section{QWidget: qt.NewQWidget2()}
	s.layout = qt.NewQVBoxLayout(s.QWidget)
	s.layout.SetContentsMargins(0, 0, 0, 0)
	s.layout.SetSpacing(6)

	heading := qt.NewQHBoxLayout2()
	label := qt.NewQLabel3(title)
	font := qt.NewQFont5(label.Font())
	font.SetBold(true)
	label.SetFont(font)
	font.Delete()
	heading.AddWidget(label.QWidget)
	s.errors = qt.NewQLabel2()
	s.errors.SetVisible(false)
	heading.AddWidget2(s.errors.QWidget, 1)
	s.layout.AddLayout(heading.QLayout)
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
	w.cpu.layout.AddWidget2(w.cpu.graph.QWidget, 1)
	w.cpu.layout.AddWidget(w.cpu.legend.QWidget)
	w.cpu.layout.AddWidget(w.cpu.total.QWidget)
}

// makeMemory makes the section of memory and swap.
func (w *Window) makeMemory() {
	w.memory.section = newSection("Memory and Swap")
	w.memory.graph = newGraph()
	w.memory.legend = newLegend(true)
	w.memory.layout.AddWidget2(w.memory.graph.QWidget, 1)
	w.memory.layout.AddWidget(w.memory.legend.QWidget)
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

	graphs := qt.NewQHBoxLayout2()
	graphs.SetSpacing(16)
	graphs.AddWidget2(w.network.received.QWidget, 1)
	graphs.AddWidget2(w.network.sent.QWidget, 1)
	w.network.layout.AddLayout2(graphs.QLayout, 1)
	w.network.layout.AddWidget(w.network.legend.QWidget)
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
	add := func(series *history.Series, v float64) {
		series.Fill(now, s, 0)
		series.Add(now, v)
		series.Trim(now.Add(-s))
	}
	add(&w.cpu.all, all)
	for i := range w.cpu.cores {
		// A processor that is no longer read, as one taken offline, is
		// idle.
		busy := 0.0
		if i < len(cores) {
			busy = cores[i]
		}
		add(&w.cpu.cores[i], busy)
	}
	w.showCPU()
}

// showCPU draws the graph and legend of the processors, as the settings
// say: a line for each, or one for all of them.
func (w *Window) showCPU() {
	colours := w.config.Colours
	var lines []line
	var entries []entry
	if w.config.CPUMode == settings.CPUTotal {
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
	total := w.config.CPUMode == settings.CPUTotal
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
	for _, l := range []struct {
		series *history.Series
		v      float64
	}{{&w.memory.memory, float64(m.Used)}, {&w.memory.swap, float64(m.SwapUsed)}} {
		l.series.Fill(now, s, l.v)
		l.series.Add(now, l.v)
	}
	before := now.Add(-s)
	w.memory.memory.Trim(before)
	w.memory.swap.Trim(before)
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
	// The graph is of how much is in use, up to how much memory there is,
	// or to more swap than that if it is in use.
	s := span(w.config.Intervals.Memory)
	since := w.now().Add(-s)
	top := max(float64(m.Total), w.memory.memory.Max(since), w.memory.swap.Max(since), 1)
	u := w.units()
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
	for name, rate := range w.network.usage.Update(now, counters) {
		a := w.network.adapters[name]
		if a == nil {
			a = &adapter{}
			w.network.adapters[name] = a
		}
		a.present, a.rate = true, rate
		// Until an adapter's second reading its lines are 0 across the
		// graphs.
		a.received.Fill(now, s, 0)
		a.sent.Fill(now, s, 0)
		a.received.Add(now, rate.Received)
		a.sent.Add(now, rate.Sent)
		if _, ok := w.config.Colours.Adapter(name); ok {
			added = true
		}
	}
	before := now.Add(-s)
	for name, a := range w.network.adapters {
		a.received.Trim(before)
		a.sent.Trim(before)
		// An adapter that has gone is forgotten once its lines have gone
		// off the left of the graphs, which a line of one point has.
		if last, ok := a.received.Last(); !a.present && (!ok || last.T.Before(before)) {
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
	return newSettingsDialog(w.QWidget, w.config, len(w.cpu.cores), sortedKeys(w.config.Colours.Network), w.applySettings)
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
		timer    *qt.QTimer
		interval float64
	}{
		{w.cpu.timer, s.Intervals.CPU},
		{w.memory.timer, s.Intervals.Memory},
		{w.network.timer, s.Intervals.Network},
		{w.processes.timer, s.Intervals.Processes},
	} {
		if ms := int(settings.Duration(t.interval) / time.Millisecond); t.timer.Interval() != ms {
			t.timer.SetInterval(ms)
		}
	}
	// The frame rate is put into effect once the window has started.
	if w.started {
		w.setFrameRate(s.FrameRate)
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
func appIcon() *qt.QIcon {
	pixmap := qt.NewQPixmap()
	// The icon keeps a copy of the picture.
	defer pixmap.Delete()
	pixmap.LoadFromDataWithData(appicon.PNG)
	return qt.NewQIcon2(pixmap)
}
