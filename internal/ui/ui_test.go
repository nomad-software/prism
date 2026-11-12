package ui

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"
	"github.com/nomad-software/qute/qutetest"

	"github.com/nomad-software/prism/internal/chart"
	"github.com/nomad-software/prism/internal/settings"
	"github.com/nomad-software/prism/internal/stats"
)

// TestMain runs the tests on goroutines of their own while Qt's event loop
// runs on the main thread, as it has to; the tests do what they do with Qt
// on it through qutetest.Run. They draw offscreen, whatever display there
// is, and closing a window does not end the event loop.
func TestMain(m *testing.M) {
	os.Setenv("QT_QPA_PLATFORM", "offscreen")
	qutetest.TestMain(m)
}

// fakeSource is a source of statistics that tests set.
type fakeSource struct {
	sync.Mutex
	cpu     stats.CPU
	memory  stats.Memory
	network map[string]stats.NetCounters
	procs   []stats.Process
	err     error
	// signalErr is what Signal returns, and signalled what it was asked
	// to send, as "pid number".
	signalErr error
	signalled []string
}

func (f *fakeSource) CPU() (stats.CPU, error) {
	f.Lock()
	defer f.Unlock()
	return f.cpu, f.err
}

func (f *fakeSource) Memory() (stats.Memory, error) {
	f.Lock()
	defer f.Unlock()
	return f.memory, f.err
}

func (f *fakeSource) Network() (map[string]stats.NetCounters, error) {
	f.Lock()
	defer f.Unlock()
	return f.network, f.err
}

func (f *fakeSource) Processes() ([]stats.Process, error) {
	f.Lock()
	defer f.Unlock()
	return slices.Clone(f.procs), f.err
}

func (f *fakeSource) Signal(p stats.Process, number int) error {
	f.Lock()
	defer f.Unlock()
	f.signalled = append(f.signalled, fmt.Sprintf("%d %d", p.PID, number))
	return f.signalErr
}

// set changes the source while it is locked.
func (f *fakeSource) set(change func(f *fakeSource)) {
	f.Lock()
	defer f.Unlock()
	change(f)
}

// testWindow is a window, with its settings in a folder of its own, reading
// from a fake source, at a time the test sets.
type testWindow struct {
	*Window
	source *fakeSource
	path   string
	clock  time.Time
}

// start is the time windows of tests start at.
var start = time.Unix(1_700_000_000, 0)

// newTestWindow makes a window, with settings changed by configure if it is
// not nil, and shows it without its timers running.
func newTestWindow(t testing.TB, configure func(*settings.Settings)) *testWindow {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	// The tests read sizes and rates in binary units of bytes, which are
	// not the defaults, so that their figures are round.
	s := settings.Defaults()
	s.SizeUnits, s.RateUnits = settings.SizeBinary, settings.RateBytes
	if configure != nil {
		configure(&s)
	}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	tw := &testWindow{source: &fakeSource{}, path: path, clock: start}
	qutetest.Run(t, func() {
		tw.Window = NewWindow(path, tw.source)
		tw.now = func() time.Time { return tw.clock }
		tw.Resize(900, 820)
		tw.Show()
	})
	t.Cleanup(func() {
		qutetest.Run(t, func() {
			tw.Close()
			tw.DeleteLater()
			applyTheme("")
		})
	})
	return tw
}

// advance moves the window's clock on.
func (tw *testWindow) advance(d time.Duration) {
	tw.clock = tw.clock.Add(d)
}

func TestNewWindow(t *testing.T) {
	tw := newTestWindow(t, nil)
	qutetest.Run(t, func() {
		if title := tw.WindowTitle(); title != "Prism" {
			t.Errorf("title = %q", title)
		}
		d := settings.DefaultIntervals
		for name, timer := range map[string]struct {
			*qtcore.QTimer
			interval float64
		}{"CPU": {tw.cpu.timer, d.CPU}, "memory": {tw.memory.timer, d.Memory}, "network": {tw.network.timer, d.Network}, "processes": {tw.processes.timer, d.Processes}} {
			if ms := int(settings.Duration(timer.interval) / time.Millisecond); timer.Interval() != ms {
				t.Errorf("%s timer interval = %d, want %d", name, timer.Interval(), ms)
			}
			if timer.IsActive() {
				t.Errorf("%s timer runs before Start", name)
			}
		}
		if tw.frames.IsActive() {
			t.Error("the graphs are animated before Start")
		}
		var menus []string
		for _, a := range tw.MenuBar().Actions() {
			menus = append(menus, a.Text())
		}
		if !slices.Equal(menus, []string{"&File", "&Help"}) {
			t.Fatalf("menus = %q", menus)
		}
		items := func(m *qtwidgets.QMenu) []string {
			var items []string
			for _, a := range m.Actions() {
				if !a.IsSeparator() {
					items = append(items, a.Text())
				}
			}
			return items
		}
		if got := items(tw.file); !slices.Equal(got, []string{"&Settings…", "&Quit"}) {
			t.Errorf("File menu = %q", got)
		}
		if got := items(tw.help); !slices.Equal(got, []string{"&About"}) {
			t.Errorf("Help menu = %q", got)
		}
		if !tw.StatusBar().IsSizeGripEnabled() {
			t.Error("the status bar has no handle to drag")
		}
	})
}

func TestStart(t *testing.T) {
	tw := newTestWindow(t, func(s *settings.Settings) { s.Maximized = true })
	tw.source.set(func(f *fakeSource) {
		f.cpu = stats.CPU{All: stats.CPUTimes{Busy: 1, Idle: 1}}
		f.memory = stats.Memory{Total: 100, Used: 50}
		f.network = map[string]stats.NetCounters{"eth0": {}}
		f.procs = []stats.Process{{PID: 1, Name: "init"}}
	})
	qutetest.Run(t, func() {
		tw.Start()
		defer func() {
			for _, timer := range []*qtcore.QTimer{tw.cpu.timer, tw.memory.timer, tw.network.timer, tw.processes.timer} {
				if !timer.IsActive() {
					t.Error("a timer is not running after Start")
				}
				timer.Stop()
			}
			// As often as the lines move by a pixel.
			if !tw.frames.IsActive() || tw.frames.Interval() != tw.frameInterval() {
				t.Errorf("frames every %d ms, running %v, want %d", tw.frames.Interval(), tw.frames.IsActive(), tw.frameInterval())
			}
			tw.frames.Stop()
		}()
		if !tw.IsMaximized() && !tw.IsFullScreen() && tw.WindowState()&qtcore.WindowMaximized == 0 {
			t.Error("the window is not maximised, as the settings say it was")
		}
		// Every graph is drawn across from the first reading: memory at
		// what it is, and the processors and adapters at 0 until the
		// second.
		if len(tw.memory.memory.Points()) == 0 {
			t.Error("memory was not read at Start")
		}
		if p := tw.cpu.all.Points(); len(p) != 2 || p[0].V != 0 || p[1].T.Sub(p[0].T) != time.Minute {
			t.Errorf("the processors are not a line of 0 across the graph: %+v", p)
		}
		if _, ok := tw.network.adapters["eth0"]; !ok {
			t.Error("the adapters were not read at Start")
		}
		if len(tw.processes.list) != 1 {
			t.Error("the processes were not read at Start")
		}
	})
}

// menuAction returns the action of the menu bar's menus with the text.
func menuAction(t *testing.T, tw *testWindow, text string) *qtgui.QAction {
	t.Helper()
	for _, m := range []*qtwidgets.QMenu{tw.file, tw.help} {
		for _, a := range m.Actions() {
			if a.Text() == text {
				return a
			}
		}
	}
	t.Fatalf("the menus have no %s", text)
	return nil
}

func TestMenuShortcuts(t *testing.T) {
	tw := newTestWindow(t, nil)
	qutetest.Run(t, func() {
		for text, want := range map[string]string{"&Settings…": "Ctrl+,", "&Quit": "Ctrl+Q", "&About": "Ctrl+I"} {
			// The shortcut is a copy, which the garbage collector frees.
			if got := menuAction(t, tw, text).Shortcut().ToString(); got != want {
				t.Errorf("%s shortcut = %q, want %q", text, got, want)
			}
		}
	})
}

func TestAboutAction(t *testing.T) {
	tw := newTestWindow(t, nil)
	qutetest.Run(t, func() {
		menuAction(t, tw, "&About").Trigger()
		var about *qtwidgets.QWidget
		for _, w := range qtwidgets.QApplicationTopLevelWidgets() {
			if w.WindowTitle() == "About Prism" && w.IsVisible() {
				about = w
			}
		}
		if about == nil {
			t.Fatal("the about box is not shown")
		}
		about.Close()
	})
}

func TestPlainVersion(t *testing.T) {
	for version, want := range map[string]string{
		"v1.15.0":                               "v1.15.0",
		"v1.15.0+dirty":                         "v1.15.0",
		"v1.15.1-0.20261027184954-00c19a9187bb": "v1.15.0",
		"v1.15.1-0.20261027184954-00c19a9187bb+dirty": "v1.15.0",
		"v2.0.0-rc.1": "v2.0.0",
		"v1.2.3-rc.1.0.20261027184954-00c19a9187bb": "v1.2.3",
		"v0.0.0-20261027184954-00c19a9187bb":        "v0.0.0",
	} {
		if got := plainVersion(version); got != want {
			t.Errorf("plainVersion(%q) = %q, want %q", version, got, want)
		}
	}
}

func TestAbout(t *testing.T) {
	qutetest.Run(t, func() {
		d := newAboutDialog(nil)
		defer d.DeleteLater()
		if d.version.Text() != appVersion() {
			t.Errorf("the version shown is %q", d.version.Text())
		}
		licences := licenceText()
		for _, want := range []string{qtcore.QLibraryInfoVersion().ToString(), "LGPLv3", "qute", "Solarized"} {
			if !strings.Contains(licences, want) {
				t.Errorf("the licences do not mention %s", want)
			}
		}
		if !strings.Contains(aboutText(), "GNU General Public License") {
			t.Error("the about box does not give the program's licence")
		}
		// It says what the program shows, the processes as well as the
		// graphs.
		for _, want := range []string{"processors", "memory and swap", "network adapters", "processes"} {
			if !strings.Contains(aboutText(), want) {
				t.Errorf("the about box does not say it shows %s", want)
			}
		}
		d.Layout().Activate()
		if need := d.Layout().AsQLayoutItem().HeightForWidth(d.Width()); d.Height() < need {
			t.Errorf("the about box is %d high, and needs %d", d.Height(), need)
		}
	})
}

func TestQuit(t *testing.T) {
	tw := newTestWindow(t, nil)
	qutetest.Run(t, func() {
		var quit *qtgui.QAction
		for _, a := range tw.file.Actions() {
			if a.Text() == "&Quit" {
				quit = a
			}
		}
		tw.Resize(800, 700)
		quit.Trigger()
		if tw.IsVisible() {
			t.Error("the window is still shown after Quit")
		}
	})
	s := settings.Load(tw.path)
	if s.Width != 800 || s.Height != 700 {
		t.Errorf("saved size = %dx%d, want 800x700", s.Width, s.Height)
	}
}

func TestSampleCPU(t *testing.T) {
	tw := newTestWindow(t, nil)
	tw.source.set(func(f *fakeSource) {
		f.cpu = stats.CPU{
			All:   stats.CPUTimes{Busy: 100, Idle: 100},
			Cores: []stats.CPUTimes{{Busy: 50, Idle: 50}, {Busy: 50, Idle: 50}},
		}
	})
	qutetest.Run(t, func() {
		tw.sampleCPU()
		// The cores are listed from the first reading, without their use.
		entries := tw.cpu.legend.entries
		if len(entries) != 2 || entries[0].label != "CPU1" || entries[0].value != "—" {
			t.Errorf("legend after one reading = %+v", entries)
		}
	})
	tw.source.set(func(f *fakeSource) {
		f.cpu = stats.CPU{
			All:   stats.CPUTimes{Busy: 150, Idle: 150},
			Cores: []stats.CPUTimes{{Busy: 75, Idle: 75}, {Busy: 75, Idle: 75}},
		}
		f.cpu.Cores[1] = stats.CPUTimes{Busy: 100, Idle: 50}
	})
	tw.advance(2 * time.Second)
	qutetest.Run(t, func() {
		tw.sampleCPU()
		if len(tw.cpu.cores) != 2 {
			t.Fatalf("%d cores, want 2", len(tw.cpu.cores))
		}
		entries := tw.cpu.legend.entries
		if len(entries) != 2 || entries[0].label != "CPU1" || entries[0].value != "50.0%" ||
			entries[1].label != "CPU2" || entries[1].value != "100.0%" {
			t.Errorf("legend = %+v", entries)
		}
		if entries[0].colour != settings.DefaultCore(0) {
			t.Errorf("CPU1 colour = %q", entries[0].colour)
		}
		if !tw.cpu.legend.IsVisible() || tw.cpu.total.IsVisible() {
			t.Error("the legend of each core is not the one shown")
		}
		if len(tw.cpu.graph.lines) != 2 {
			t.Errorf("graph has %d lines, want 2", len(tw.cpu.graph.lines))
		}
		if tw.cpu.graph.top != 100 || tw.cpu.graph.span != time.Minute {
			t.Errorf("graph top %v, span %v", tw.cpu.graph.top, tw.cpu.graph.span)
		}
	})

	// Points older than the span are dropped but for the newest of them.
	for range 40 {
		tw.advance(2 * time.Second)
		qutetest.Run(t, tw.sampleCPU)
	}
	qutetest.Run(t, func() {
		points := tw.cpu.all.Points()
		if first := points[0].T; first.After(tw.clock.Add(-time.Minute)) {
			t.Errorf("the first point is at %v, after the left edge", first)
		}
		if second := points[1].T; second.Before(tw.clock.Add(-time.Minute)) {
			t.Errorf("the second point is at %v, before the left edge", second)
		}
	})
}

func TestCPUTotalMode(t *testing.T) {
	tw := newTestWindow(t, func(s *settings.Settings) {
		s.CPUMode = settings.CPUTotal
		s.Colours.Total = "#123456"
	})
	tw.source.set(func(f *fakeSource) {
		f.cpu = stats.CPU{All: stats.CPUTimes{Busy: 0, Idle: 0}, Cores: []stats.CPUTimes{{}, {}}}
	})
	qutetest.Run(t, tw.sampleCPU)
	tw.source.set(func(f *fakeSource) {
		f.cpu = stats.CPU{All: stats.CPUTimes{Busy: 1, Idle: 3}, Cores: []stats.CPUTimes{{}, {}}}
	})
	tw.advance(time.Second)
	qutetest.Run(t, func() {
		tw.sampleCPU()
		entries := tw.cpu.total.entries
		if len(entries) != 1 || !sameEntry(entries[0], entry{"#123456", "CPU", "25.0%", nil}) {
			t.Errorf("legend = %+v", entries)
		}
		if tw.cpu.legend.IsVisible() || !tw.cpu.total.IsVisible() {
			t.Error("the legend of all cores is not the one shown")
		}
		if len(tw.cpu.graph.lines) != 1 {
			t.Errorf("graph has %d lines, want 1", len(tw.cpu.graph.lines))
		}
	})
}

func TestReadErrors(t *testing.T) {
	tw := newTestWindow(t, nil)
	tw.source.set(func(f *fakeSource) { f.err = errors.New("no such file") })
	qutetest.Run(t, func() {
		tw.sampleCPU()
		tw.sampleMemory()
		tw.sampleNetwork()
		for name, s := range map[string]*section{"CPU": &tw.cpu.section, "memory": &tw.memory.section, "network": &tw.network.section} {
			if !s.errors.IsVisible() || s.errors.Text() != "— no such file" {
				t.Errorf("%s error = %q, shown %v", name, s.errors.Text(), s.errors.IsVisible())
			}
		}
	})
	tw.source.set(func(f *fakeSource) {
		f.err = nil
		f.memory = stats.Memory{Total: 1}
	})
	qutetest.Run(t, func() {
		tw.sampleMemory()
		if tw.memory.errors.IsVisible() {
			t.Error("the error is still shown after a reading that worked")
		}
	})
}

func TestSampleMemory(t *testing.T) {
	tw := newTestWindow(t, nil)
	const gib = 1 << 30
	tw.source.set(func(f *fakeSource) {
		f.memory = stats.Memory{Total: 16 * gib, Used: 4 * gib}
	})
	qutetest.Run(t, func() {
		tw.sampleMemory()
		want := []entry{
			{settings.DefaultMemory, "Memory", "4.0 GiB (25.0%)", nil},
			{settings.DefaultSwap, "Swap", "Not available", nil},
		}
		got := tw.memory.legend.entries
		if !tw.memory.legend.faint {
			t.Error("the use of memory is not faint")
		}
		if len(got) != 2 || !sameEntry(got[0], want[0]) || !sameEntry(got[1], want[1]) {
			t.Errorf("legend = %+v, want %+v", got, want)
		}

		// Its swatch sets the colour, which is applied and saved at once.
		got[0].setColour("#010203")
		if tw.config.Colours.Memory != "#010203" || tw.memory.legend.entries[0].colour != "#010203" {
			t.Error("the colour set by the swatch is not shown")
		}
		if saved := settings.Load(tw.path).Colours.Memory; saved != "#010203" {
			t.Errorf("saved memory colour = %q", saved)
		}
		if p, _ := tw.memory.memory.Last(); p.V != 4*gib {
			t.Errorf("memory = %v, want 4 GiB", p.V)
		}
		// The graph is to the scale of the memory there is.
		g := tw.memory.graph
		if g.top != 16*gib || g.labels[0] != "0 B" || g.labels[len(g.labels)-1] != "16.0 GiB" {
			t.Errorf("memory graph top %v, labels %q", g.top, g.labels)
		}
		if p, _ := tw.memory.swap.Last(); p.V != 0 {
			t.Errorf("swap = %v, want 0", p.V)
		}
	})
}

// TestAdapterColoursOrdered checks that adapters seen for the first time in
// the same reading are given colours in the order of their names, the same
// each time, not in whatever order a map gives them.
func TestAdapterColoursOrdered(t *testing.T) {
	names := []string{"wlan0", "lo", "enp5s0", "docker0", "virbr0", "eth1"}
	want := settings.Defaults().Colours.Clone()
	for _, name := range slices.Sorted(slices.Values(names)) {
		want.Adapter(name)
	}
	// Maps are iterated in another order each time, so a few windows would
	// tell colours given in map order.
	for range 5 {
		tw := newTestWindow(t, nil)
		tw.source.set(func(f *fakeSource) {
			f.network = map[string]stats.NetCounters{}
			for _, name := range names {
				f.network[name] = stats.NetCounters{}
			}
		})
		qutetest.Run(t, tw.sampleNetwork)
		if got := settings.Load(tw.path).Colours.Network; !maps.Equal(got, want.Network) {
			t.Fatalf("colours = %v, want %v", got, want.Network)
		}
	}
}

func TestSampleNetwork(t *testing.T) {
	tw := newTestWindow(t, nil)
	tw.source.set(func(f *fakeSource) {
		f.network = map[string]stats.NetCounters{"lo": {Received: 0, Sent: 0}, "eth0": {Received: 1000, Sent: 0}}
	})
	qutetest.Run(t, tw.sampleNetwork)

	// The colours of the adapters are given and saved at once.
	saved := settings.Load(tw.path).Colours.Network
	if len(saved) != 2 || saved["eth0"] == saved["lo"] {
		t.Errorf("saved colours = %v, want one for each adapter", saved)
	}
	qutetest.Run(t, func() {
		if p := tw.network.adapters["eth0"].received.Points(); len(p) != 2 || p[0].V != 0 || p[1].V != 0 {
			t.Errorf("an adapter is not a line of 0 after a single reading: %+v", p)
		}
	})

	tw.source.set(func(f *fakeSource) {
		f.network = map[string]stats.NetCounters{"lo": {Received: 0, Sent: 0}, "eth0": {Received: 3 * 1024 * 1024, Sent: 2048}}
	})
	tw.advance(2 * time.Second)
	qutetest.Run(t, func() {
		tw.sampleNetwork()
		entries := tw.network.legend.entries
		if len(entries) != 2 || entries[0].label != "eth0" || entries[1].label != "lo" {
			t.Fatalf("legend = %+v", entries)
		}
		if !tw.network.legend.faint {
			t.Error("the rates of the adapters are not faint")
		}
		if want := "↓ 1.5 MiB/s (3.0 MiB)   ↑ 1.0 KiB/s (2.0 KiB)"; entries[0].value != want {
			t.Errorf("eth0 = %q, want %q", entries[0].value, want)
		}
		if entries[0].colour != saved["eth0"] {
			t.Errorf("eth0 colour = %q, want %q", entries[0].colour, saved["eth0"])
		}
		if top := tw.network.received.top; top != 2*1024*1024 {
			t.Errorf("received top = %v, want 2 MiB/s", top)
		}
		if top := tw.network.sent.top; top != 1024 {
			t.Errorf("sent top = %v, want 1 KiB/s", top)
		}
		if len(tw.network.received.lines) != 2 || len(tw.network.sent.lines) != 2 {
			t.Error("the graphs do not have a line for each adapter")
		}
	})

	// An adapter that goes away leaves the legend at once, and the graphs
	// once its line has gone off them; its colour is kept.
	tw.source.set(func(f *fakeSource) {
		f.network = map[string]stats.NetCounters{"lo": {Received: 0, Sent: 0}}
	})
	tw.advance(2 * time.Second)
	qutetest.Run(t, func() {
		tw.sampleNetwork()
		if len(tw.network.legend.entries) != 1 {
			t.Errorf("legend = %+v, want only lo", tw.network.legend.entries)
		}
		if _, ok := tw.network.adapters["eth0"]; !ok {
			t.Error("eth0 was forgotten while its line is on the graphs")
		}
	})
	tw.advance(2 * time.Minute)
	qutetest.Run(t, func() {
		tw.sampleNetwork()
		if _, ok := tw.network.adapters["eth0"]; ok {
			t.Error("eth0 is kept after its line has gone off the graphs")
		}
		if len(tw.network.received.lines) != 1 {
			t.Errorf("received graph has %d lines, want 1", len(tw.network.received.lines))
		}
	})
	if saved := settings.Load(tw.path).Colours.Network; len(saved) != 2 {
		t.Errorf("saved colours = %v, want eth0's kept", saved)
	}
}

func TestHiddenAdapters(t *testing.T) {
	tw := newTestWindow(t, func(s *settings.Settings) { s.HiddenAdapters = []string{"lo"} })
	tw.source.set(func(f *fakeSource) {
		f.network = map[string]stats.NetCounters{"lo": {Received: 0}, "eth0": {Received: 0}}
	})
	qutetest.Run(t, tw.sampleNetwork)
	tw.source.set(func(f *fakeSource) {
		f.network = map[string]stats.NetCounters{"lo": {Received: 1 << 30}, "eth0": {Received: 1024}}
	})
	tw.advance(time.Second)
	qutetest.Run(t, func() {
		tw.sampleNetwork()
		entries := tw.network.legend.entries
		if len(entries) != 1 || entries[0].label != "eth0" {
			t.Errorf("legend = %+v, want only eth0", entries)
		}
		if len(tw.network.received.lines) != 1 {
			t.Errorf("received graph has %d lines, want 1", len(tw.network.received.lines))
		}
		// The scale is that of what is shown, not of lo's gibibyte.
		if top := tw.network.received.top; top != 1024 {
			t.Errorf("received top = %v, want 1 KiB/s", top)
		}

		// Shown again, its line is whole, as it was read all along.
		s := tw.config
		s.HiddenAdapters = nil
		tw.applySettings(s)
		if len(tw.network.received.lines) != 2 || len(tw.network.legend.entries) != 2 {
			t.Error("lo is not shown once it is no longer hidden")
		}

		// An adapter with an alias is shown by it.
		s.AdapterAliases = map[string]string{"lo": "Loopback"}
		tw.applySettings(s)
		if entries := tw.network.legend.entries; len(entries) != 2 || entries[0].label != "eth0" || entries[1].label != "Loopback" {
			t.Errorf("legend = %+v, want lo shown as Loopback", entries)
		}

		// The units are those the settings choose.
		s.SizeUnits, s.RateUnits = settings.SizeDecimal, settings.RateBits
		tw.applySettings(s)
		if want := "↓ 8.2 kb/s (1.0 kB)   ↑ 0 bits/s (0 B)"; tw.network.legend.entries[0].value != want {
			t.Errorf("eth0 = %q, want %q", tw.network.legend.entries[0].value, want)
		}
		// lo's gibibyte a second, 8.6 Gb/s, is drawn to 10 Gb/s.
		if top := tw.network.received.top; top != 10e9/8 {
			t.Errorf("received top = %v, want 10 Gb/s", top)
		}
	})
}

func TestApplySettings(t *testing.T) {
	tw := newTestWindow(t, nil)
	tw.source.set(func(f *fakeSource) { f.memory = stats.Memory{Total: 100, Used: 10} })
	qutetest.Run(t, func() {
		tw.sampleMemory()
		tw.Resize(800, 750)
		s := tw.config
		s.Width, s.Height = 1, 1
		s.Theme = settings.ThemeDark
		s.Intervals = settings.Intervals{CPU: 0.5, Memory: 90, Network: 3600, Processes: 1.5}
		s.Colours.Memory = "#010203"
		tw.applySettings(s)

		// The graphs are not animated before Start.
		if tw.frames.IsActive() {
			t.Error("the graphs are animated before Start")
		}
		if tw.cpu.timer.Interval() != 500 || tw.memory.timer.Interval() != 90_000 || tw.network.timer.Interval() != 3_600_000 || tw.processes.timer.Interval() != 1500 {
			t.Errorf("intervals = %d, %d, %d, %d", tw.cpu.timer.Interval(), tw.memory.timer.Interval(), tw.network.timer.Interval(), tw.processes.timer.Interval())
		}
		if tw.memory.graph.span != 3*time.Minute {
			t.Errorf("memory span = %v, want 3m", tw.memory.graph.span)
		}
		if tw.memory.legend.entries[0].colour != "#010203" {
			t.Errorf("memory colour = %q", tw.memory.legend.entries[0].colour)
		}
		if c := qtgui.QGuiApplicationPalette().Color(qtgui.QPaletteWindow).Name(); c != "#2d2d2d" {
			t.Errorf("window colour = %s, want the dark theme's", c)
		}
	})
	s := settings.Load(tw.path)
	if s.Theme != settings.ThemeDark || s.Intervals.Memory != 90 || s.Colours.Memory != "#010203" {
		t.Errorf("saved = %+v", s)
	}
	if s.Width != 800 || s.Height != 750 {
		t.Errorf("saved size = %dx%d, want the window's", s.Width, s.Height)
	}
}

func TestAnimation(t *testing.T) {
	tw := newTestWindow(t, nil)
	tw.source.set(func(f *fakeSource) {
		f.memory = stats.Memory{Total: 100, Used: 10}
		f.network = map[string]stats.NetCounters{"eth0": {}}
	})
	qutetest.Run(t, func() {
		tw.Start()
		defer func() {
			for _, timer := range []*qtcore.QTimer{tw.cpu.timer, tw.memory.timer, tw.network.timer, tw.processes.timer, tw.frames} {
				timer.Stop()
			}
		}()
		// A graph's lines move by a pixel in the time it spans over its
		// width in pixels, and the frames come as often as the fastest
		// moves.
		g := tw.cpu.graph
		g.Resize(600, 200)
		if want := time.Minute / time.Duration(600*g.AsQPaintDevice().DevicePixelRatioF()); g.pixelTime() != want {
			t.Errorf("pixel time = %v, want %v", g.pixelTime(), want)
		}
		fastest := maxFrame
		for _, g := range tw.graphs() {
			fastest = min(fastest, g.pixelTime())
		}
		tw.animate()
		if want := int(max(fastest, minFrame) / time.Millisecond); tw.frames.Interval() != want {
			t.Errorf("frames every %d ms, want %d", tw.frames.Interval(), want)
		}
		// A graph so wide that its lines move faster than 60 frames a
		// second could show is drawn no more often than that, and one so
		// narrow that they hardly move is still moved each second.
		g.Resize(100_000, 200)
		tw.animate()
		if want := int(minFrame / time.Millisecond); tw.frames.Interval() != want {
			t.Errorf("frames every %d ms of a wide graph, want %d", tw.frames.Interval(), want)
		}
		for _, g := range tw.graphs() {
			g.Resize(1, 200)
		}
		tw.animate()
		if want := int(maxFrame / time.Millisecond); tw.frames.Interval() != want {
			t.Errorf("frames every %d ms of narrow graphs, want %d", tw.frames.Interval(), want)
		}

		// A frame moves every graph on to now, and their lines hold. The
		// scales and legends are left as the last reading had them.
		top, value := tw.memory.graph.top, tw.memory.legend.entries[0].value
		tw.advance(time.Second)
		tw.animate()
		if tw.memory.graph.top != top || tw.memory.legend.entries[0].value != value {
			t.Error("a frame changed the scale or the legend")
		}
		for _, g := range []*graph{tw.cpu.graph, tw.memory.graph, tw.network.received, tw.network.sent} {
			if !g.now.Equal(tw.clock) {
				t.Errorf("a graph is at %v, want %v", g.now, tw.clock)
			}
			for _, l := range g.lines {
				if !l.hold {
					t.Error("a line does not hold")
				}
			}
		}
		// An adapter that has gone does not.
		tw.source.set(func(f *fakeSource) { f.network = map[string]stats.NetCounters{} })
		tw.sampleNetwork()
		if l := tw.network.received.lines; len(l) != 1 || l[0].hold {
			t.Errorf("lines = %+v, want the gone adapter's not held", l)
		}
	})
}

func TestEditSettings(t *testing.T) {
	tw := newTestWindow(t, nil)
	qutetest.Run(t, func() {
		d := tw.editSettings()
		if !d.IsVisible() {
			t.Error("the settings are not shown")
		}
		d.memoryInterval.SetValue(4.2)
		d.Accept()
		// Accept does not go through the buttons; they are tested with
		// the dialog itself.
		tw.applySettings(d.chosen(tw.config))
		if tw.config.Intervals.Memory != 4.2 {
			t.Errorf("memory interval = %v", tw.config.Intervals.Memory)
		}
	})
}

func TestPercent(t *testing.T) {
	if p := percent(1, 4); p != 25 {
		t.Errorf("percent(1, 4) = %v", p)
	}
	if p := percent(1, 0); p != 0 {
		t.Errorf("percent(1, 0) = %v", p)
	}
}

func TestUsedOf(t *testing.T) {
	if s := usedOf(chart.Units{}, 512, 2048); s != "512 B (25.0%)" {
		t.Errorf("usedOf = %q", s)
	}
	if s := usedOf(chart.Units{}, 0, 0); s != "Not available" {
		t.Errorf("usedOf of nothing = %q", s)
	}
}

func TestSortedKeys(t *testing.T) {
	got := sortedKeys(map[string]int{"b": 1, "a": 2, "c": 3})
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("sortedKeys = %v", got)
	}
}

func TestApplyTheme(t *testing.T) {
	t.Cleanup(func() { qutetest.Run(t, func() { applyTheme("") }) })
	qutetest.Run(t, func() {
		window := func() string {
			return qtgui.QGuiApplicationPalette().Color(qtgui.QPaletteWindow).Name()
		}
		applyTheme(settings.ThemeLight)
		if c := window(); c != "#efefef" {
			t.Errorf("light window = %s", c)
		}
		if s := qtwidgets.QApplicationStyle().Name(); s != "fusion" {
			t.Errorf("light style = %s", s)
		}
		applyTheme(settings.ThemeDark)
		if c := window(); c != "#2d2d2d" {
			t.Errorf("dark window = %s", c)
		}
		applyTheme("")
		if s := qtwidgets.QApplicationStyle().Name(); s != systemStyle {
			t.Errorf("system style = %s, want %s", s, systemStyle)
		}
	})
}

func TestMakePalette(t *testing.T) {
	qutetest.Run(t, func() {
		p := makePalette(darkColours)
		defer p.Delete()
		if c := p.GroupColor(qtgui.QPaletteDisabled, qtgui.QPaletteText).Name(); c != "#7a7a7a" {
			t.Errorf("disabled text = %s", c)
		}
	})
}

// sameEntry says whether two entries of a legend show the same.
func sameEntry(a, b entry) bool {
	return a.colour == b.colour && a.label == b.label && a.value == b.value
}

func TestLegendColourSetters(t *testing.T) {
	tw := newTestWindow(t, nil)
	tw.source.set(func(f *fakeSource) {
		f.cpu = stats.CPU{Cores: []stats.CPUTimes{{}, {}}}
		f.memory = stats.Memory{Total: 100, Used: 10}
		f.network = map[string]stats.NetCounters{"eth0": {}}
	})
	qutetest.Run(t, func() {
		tw.sampleCPU()
		tw.sampleMemory()
		tw.sampleNetwork()

		cores := tw.cpu.legend.entries
		cores[0].setColour("#111111")
		// A core set to its default is kept as no choice.
		cores[1].setColour(settings.DefaultCore(1))
		tw.memory.legend.entries[1].setColour("#222222")
		tw.network.legend.entries[0].setColour("#333333")

		s := tw.config
		s.CPUMode = settings.CPUTotal
		tw.applySettings(s)
		tw.cpu.total.entries[0].setColour("#444444")
	})
	for name, s := range map[string]settings.Settings{"applied": tw.config, "saved": settings.Load(tw.path)} {
		c := s.Colours
		if len(c.Cores) != 2 || c.Cores[0] != "#111111" || c.Cores[1] != "" {
			t.Errorf("%s Cores = %q", name, c.Cores)
		}
		if c.Swap != "#222222" || c.Network["eth0"] != "#333333" || c.Total != "#444444" {
			t.Errorf("%s colours = %+v", name, c)
		}
	}
	qutetest.Run(t, func() {
		if tw.cpu.total.entries[0].colour != "#444444" || tw.network.legend.entries[0].colour != "#333333" {
			t.Error("the legends do not show the colours set")
		}
	})
}

func TestNetworkTitles(t *testing.T) {
	tw := newTestWindow(t, nil)
	qutetest.Run(t, func() {
		if tw.network.received.title != "Download" || tw.network.sent.title != "Upload" {
			t.Error("the network graphs are not titled Download and Upload")
		}
		if tw.cpu.graph.title != "" || tw.memory.graph.title != "" {
			t.Error("a graph that is not of the network has a title")
		}
	})
}

func TestSettingsAction(t *testing.T) {
	tw := newTestWindow(t, nil)
	qutetest.Run(t, func() {
		var action *qtgui.QAction
		for _, a := range tw.file.Actions() {
			if a.Text() == "&Settings…" {
				action = a
			}
		}
		action.Trigger()
		var dialog *qtwidgets.QWidget
		for _, w := range qtwidgets.QApplicationTopLevelWidgets() {
			if w.WindowTitle() == "Settings" && w.IsVisible() {
				dialog = w
			}
		}
		if dialog == nil {
			t.Fatal("the settings are not shown")
		}
		dialog.Close()
	})
}

func TestSaveConfigFails(t *testing.T) {
	tw := newTestWindow(t, nil)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr := os.Stderr
	os.Stderr = w
	qutetest.Run(t, func() {
		// The settings cannot be saved in a folder that is not there.
		tw.Window.path = filepath.Join(t.TempDir(), "missing", "settings.json")
		tw.saveConfig()
	})
	os.Stderr = stderr
	w.Close()
	said, _ := io.ReadAll(r)
	if !strings.Contains(string(said), "the settings could not be saved") {
		t.Errorf("stderr = %q", said)
	}
}

func TestStartThemedNotMaximised(t *testing.T) {
	tw := newTestWindow(t, func(s *settings.Settings) { s.Theme = settings.ThemeDark })
	qutetest.Run(t, func() {
		// A theme the settings choose is in effect from the start.
		if style := qtwidgets.QApplicationStyle().Name(); !strings.EqualFold(style, "fusion") {
			t.Errorf("style = %q, want Fusion", style)
		}
		tw.Hide()
		tw.Start()
		if !tw.IsVisible() || tw.IsMaximized() {
			t.Error("the window is not shown as it was, unmaximised")
		}
		for _, timer := range []*qtcore.QTimer{tw.cpu.timer, tw.memory.timer, tw.network.timer, tw.processes.timer} {
			timer.Stop()
		}
	})
}

func TestMemoryScaleFitsSwap(t *testing.T) {
	tw := newTestWindow(t, nil)
	const gib = 1 << 30
	tw.source.set(func(f *fakeSource) {
		f.memory = stats.Memory{Total: 2 * gib, Used: gib, SwapTotal: 8 * gib, SwapUsed: 3 * gib}
	})
	qutetest.Run(t, func() {
		tw.sampleMemory()
		// More swap in use than there is memory raises the scale to it.
		if top := tw.memory.graph.top; top != 3*gib {
			t.Errorf("memory graph top = %v, want 3 GiB", top)
		}
		if got := tw.memory.legend.entries[1].value; got != "3.0 GiB (37.5%)" {
			t.Errorf("swap = %q", got)
		}
	})
}

func TestMemoryScale(t *testing.T) {
	tw := newTestWindow(t, nil)
	const gib = 1 << 30
	tw.source.set(func(f *fakeSource) {
		// What a computer sold with 32 GB says it has.
		f.memory = stats.Memory{Total: 33583353856, Used: 15.5e9}
	})
	qutetest.Run(t, func() {
		tw.sampleMemory()
		config := tw.config
		config.SizeUnits = settings.SizeDecimal
		tw.applySettings(config)
		// By default the scale is the power of 2 the memory comes to.
		if top := tw.memory.graph.top; top != 32e9 {
			t.Errorf("memory graph top = %v, want 32 GB", top)
		}
		if got := tw.memory.graph.labels[chart.Rows]; got != "32.0 GB" {
			t.Errorf("top label = %q", got)
		}
		if got := tw.memory.legend.entries[0].value; !strings.HasPrefix(got, "15.5 GB ") {
			t.Errorf("memory = %q", got)
		}

		// The dialog shows the default, which is not kept as a choice.
		d := tw.editSettings()
		defer d.Reject()
		if got, suffix := d.memoryMax.Value(), d.memoryMax.Suffix(); got != 32 || suffix != " GB" {
			t.Errorf("maximum memory shows %d%s", got, suffix)
		}
		if got := d.chosen(tw.config).MemoryMax; got != 0 {
			t.Errorf("the default is kept as %d", got)
		}

		// A maximum can be chosen.
		d.memoryMax.SetValue(64)
		tw.applySettings(d.chosen(tw.config))
		if tw.config.MemoryMax != 64 || tw.memory.graph.top != 64e9 {
			t.Errorf("maximum = %d, top = %v, want 64 GB", tw.config.MemoryMax, tw.memory.graph.top)
		}

		// The scale rises for more in use than a maximum chosen.
		d.memoryMax.SetValue(8)
		tw.applySettings(d.chosen(tw.config))
		if top := tw.memory.graph.top; top != 15.5e9 {
			t.Errorf("memory graph top = %v, want 15.5 GB", top)
		}

		// In binary units the same number is of GiB.
		selectData(d.sizeUnits, settings.SizeBinary)
		if got := d.memoryMax.Suffix(); got != " GiB" {
			t.Errorf("binary suffix = %q", got)
		}
		d.memoryMax.SetValue(32)
		tw.applySettings(d.chosen(tw.config))
		if top := tw.memory.graph.top; top != 32*gib {
			t.Errorf("memory graph top = %v, want 32 GiB", top)
		}

		// Defaults are restored.
		d.show(defaultsFor(tw.config))
		if got := d.memoryMax.Value(); got != 32 {
			t.Errorf("restored maximum = %d, want 32", got)
		}
	})
}

func TestSettingsListEveryCore(t *testing.T) {
	for _, cores := range []int{1, 4, 16, 64} {
		tw := newTestWindow(t, nil)
		tw.source.set(func(f *fakeSource) { f.cpu = stats.CPU{Cores: make([]stats.CPUTimes, cores)} })
		qutetest.Run(t, func() {
			tw.sampleCPU()
			d := tw.editSettings()
			defer d.Reject()
			if len(d.cores) != cores {
				t.Errorf("%d cores: the settings list %d", cores, len(d.cores))
			}
			// Each is named, with its default colour.
			for i, sw := range d.cores {
				if sw.colour != settings.DefaultCore(i) {
					t.Errorf("%d cores: CPU%d is %q", cores, i+1, sw.colour)
				}
			}
			// Below the heading and rows of the graphs, and the CPU heading and
			// all cores, is a row for each.
			last := d.colourList.Item(5+cores, colColourName)
			if want := fmt.Sprintf("CPU%d", cores); last == nil || last.Text() != want {
				t.Errorf("%d cores: the last core's row is not %s", cores, want)
			}
		})
	}
}

func TestSettingsKeepUnlistedCores(t *testing.T) {
	current := settings.Defaults()
	current.Colours.Cores = []string{"#111111", "", "", "", "#555555", "", "#777777"}
	var got *settings.Settings
	qutetest.Run(t, func() {
		// A computer with four cores, of settings from one with more.
		d := newSettingsDialog(nil, current, 4, nil, 16, func(s settings.Settings) { got = &s })
		d.cores[0].set("#aaaaaa")
		d.box.Button(qtwidgets.QDialogButtonBoxOk).Click()
	})
	want := []string{"#aaaaaa", "", "", "", "#555555", "", "#777777"}
	if got == nil || !slices.Equal(got.Colours.Cores, want) {
		t.Errorf("Cores = %q, want %q", got.Colours.Cores, want)
	}
}

func TestCPUCoresChange(t *testing.T) {
	tw := newTestWindow(t, nil)
	tw.source.set(func(f *fakeSource) {
		f.cpu = stats.CPU{All: stats.CPUTimes{Busy: 10, Idle: 10}, Cores: []stats.CPUTimes{{Busy: 10, Idle: 10}}}
	})
	qutetest.Run(t, tw.sampleCPU)

	// A processor brought online is drawn across the graph at 0 from its
	// first reading, as those there from the start were.
	tw.source.set(func(f *fakeSource) {
		f.cpu = stats.CPU{All: stats.CPUTimes{Busy: 30, Idle: 20}, Cores: []stats.CPUTimes{{Busy: 20, Idle: 20}, {Busy: 10, Idle: 0}}}
	})
	tw.advance(2 * time.Second)
	qutetest.Run(t, func() {
		tw.sampleCPU()
		if len(tw.cpu.cores) != 2 {
			t.Fatalf("%d cores, want 2", len(tw.cpu.cores))
		}
		p := tw.cpu.cores[1].Points()
		if len(p) != 2 || p[0].V != 0 || p[1].V != 0 || tw.clock.Sub(p[0].T) != time.Minute {
			t.Errorf("the new core is not a line of 0 across the graph: %+v", p)
		}
		if got := tw.cpu.legend.entries[1].value; got != "0.0%" {
			t.Errorf("CPU2 = %q, want 0.0%%", got)
		}
	})

	// One no longer read is idle, and stays listed.
	tw.source.set(func(f *fakeSource) {
		f.cpu = stats.CPU{All: stats.CPUTimes{Busy: 40, Idle: 30}, Cores: []stats.CPUTimes{{Busy: 30, Idle: 30}}}
	})
	tw.advance(2 * time.Second)
	qutetest.Run(t, func() {
		tw.sampleCPU()
		if len(tw.cpu.legend.entries) != 2 {
			t.Fatalf("legend = %+v, want both cores", tw.cpu.legend.entries)
		}
		if p, _ := tw.cpu.cores[1].Last(); p.V != 0 || !p.T.Equal(tw.clock) {
			t.Errorf("the core taken offline is at %+v, want 0 now", p)
		}
		if got := tw.cpu.legend.entries[0].value; got != "50.0%" {
			t.Errorf("CPU1 = %q, want 50.0%%", got)
		}
	})
}

func TestSaveSettingsMaximised(t *testing.T) {
	tw := newTestWindow(t, func(s *settings.Settings) { s.Width, s.Height = 500, 400 })
	maximised := false
	qutetest.Run(t, func() {
		tw.Resize(500, 400)
		tw.ShowMaximized()
		maximised = tw.IsMaximized()
		tw.saveSettings()
	})
	if !maximised {
		t.Skip("the platform does not maximise windows")
	}
	// Maximised, the size kept is the one it is restored to, not the size
	// of the screen.
	s := settings.Load(tw.path)
	if !s.Maximized || s.Width != 500 || s.Height != 400 {
		t.Errorf("saved maximised %v at %dx%d, want maximised at 500x400", s.Maximized, s.Width, s.Height)
	}
}

func TestSmallFont(t *testing.T) {
	qutetest.Run(t, func() {
		w := qtwidgets.NewQWidget(nil)
		defer w.Delete()
		for size, want := range map[float64]float64{8: 7, 12: 9, 20: 15} {
			f := qtgui.NewQFont()
			f.SetPointSizeF(size)
			w.SetFont(f)
			f.Delete()
			small := smallFont(w)
			if got := small.PointSizeF(); got != want {
				t.Errorf("smallFont of %vpt = %vpt, want %vpt", size, got, want)
			}
			small.Delete()
		}
	})
}

// labels returns every label under the object, however deep.
func labels(o qtcore.QObjectLike) []*qtwidgets.QLabel {
	var found []*qtwidgets.QLabel
	for _, child := range o.AsQObject().Children() {
		if label := qtwidgets.QLabelCast(child); label != nil {
			found = append(found, label)
		}
		found = append(found, labels(child)...)
	}
	return found
}

func TestSettingsIntervalNote(t *testing.T) {
	// The note on the intervals says what range the boxes take.
	qutetest.Run(t, func() {
		d := newSettingsDialog(nil, settings.Defaults(), 0, nil, 16, func(settings.Settings) {})
		defer d.Reject()
		found := false
		for _, label := range labels(d) {
			if strings.HasPrefix(label.Text(), "How often each is read") {
				found = true
				if !strings.Contains(label.Text(), "a tenth of a second to an hour") {
					t.Errorf("note = %q", label.Text())
				}
			}
		}
		if !found {
			t.Error("there is no note on the intervals")
		}
		if d.cpuInterval.Minimum() != settings.MinInterval || d.cpuInterval.Maximum() != settings.MaxInterval {
			t.Error("the boxes do not take the range of the settings")
		}
	})
}

func TestActivate(t *testing.T) {
	t.Setenv("XDG_ACTIVATION_TOKEN", "")
	tw := newTestWindow(t, nil)
	qutetest.Run(t, func() {
		tw.SetWindowState(qtcore.WindowMaximized | qtcore.WindowMinimized)
		tw.Show()
		tw.Activate("token-1")
		if tw.IsMinimized() || !tw.IsMaximized() || !tw.IsVisible() {
			t.Errorf("minimised: state = %v, visible %v", tw.WindowState(), tw.IsVisible())
		}
		if got := os.Getenv("XDG_ACTIVATION_TOKEN"); got != "token-1" {
			t.Errorf("token = %q", got)
		}

		tw.Hide()
		tw.Activate("")
		if !tw.IsVisible() {
			t.Error("hidden: not shown")
		}
		// No token leaves the one there is.
		if got := os.Getenv("XDG_ACTIVATION_TOKEN"); got != "token-1" {
			t.Errorf("token = %q after none", got)
		}
	})
}
