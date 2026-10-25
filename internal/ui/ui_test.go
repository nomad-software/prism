package ui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/prism/internal/chart"
	"github.com/nomad-software/prism/internal/settings"
	"github.com/nomad-software/prism/internal/stats"
)

// TestMain runs the tests on a goroutine of their own while Qt's event loop
// runs on the main thread, as it has to; the tests do what they do with Qt
// on it through mainthread.Wait.
func TestMain(m *testing.M) {
	os.Setenv("QT_QPA_PLATFORM", "offscreen")
	qt.NewQApplication(os.Args[:1])
	// Tests close windows, which must not end the event loop.
	qt.QGuiApplication_SetQuitOnLastWindowClosed(false)

	code := 0
	go func() {
		code = m.Run()
		mainthread.Start(qt.QCoreApplication_Quit)
	}()
	qt.QApplication_Exec()
	os.Exit(code)
}

// fakeSource is a source of statistics that tests set.
type fakeSource struct {
	sync.Mutex
	cpu     stats.CPU
	memory  stats.Memory
	network map[string]stats.NetCounters
	err     error
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
func newTestWindow(t *testing.T, configure func(*settings.Settings)) *testWindow {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	if configure != nil {
		s := settings.Defaults()
		configure(&s)
		if err := s.Save(path); err != nil {
			t.Fatal(err)
		}
	}
	tw := &testWindow{source: &fakeSource{}, path: path, clock: start}
	mainthread.Wait(func() {
		tw.Window = NewWindow(path, tw.source)
		tw.now = func() time.Time { return tw.clock }
		tw.Resize(900, 820)
		tw.Show()
	})
	t.Cleanup(func() {
		mainthread.Wait(func() {
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
	mainthread.Wait(func() {
		if title := tw.WindowTitle(); title != "Prism" {
			t.Errorf("title = %q", title)
		}
		ms := int(settings.Duration(settings.DefaultInterval) / time.Millisecond)
		for name, timer := range map[string]*qt.QTimer{"CPU": tw.cpu.timer, "memory": tw.memory.timer, "network": tw.network.timer} {
			if timer.Interval() != ms {
				t.Errorf("%s timer interval = %d, want %d", name, timer.Interval(), ms)
			}
			if timer.IsActive() {
				t.Errorf("%s timer runs before Start", name)
			}
		}
		var menus []string
		for _, a := range tw.MenuBar().Actions() {
			menus = append(menus, a.Text())
		}
		if !slices.Equal(menus, []string{"&File", "&Help"}) {
			t.Fatalf("menus = %q", menus)
		}
		items := func(m *qt.QMenu) []string {
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
		if got := items(tw.help); !slices.Equal(got, []string{"&About Prism"}) {
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
	})
	mainthread.Wait(func() {
		tw.Start()
		defer func() {
			for _, timer := range []*qt.QTimer{tw.cpu.timer, tw.memory.timer, tw.network.timer} {
				if !timer.IsActive() {
					t.Error("a timer is not running after Start")
				}
				timer.Stop()
			}
		}()
		if !tw.IsMaximized() && !tw.IsFullScreen() && tw.WindowState()&qt.WindowMaximized == 0 {
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
	})
}

// menuAction returns the action of the menu bar's menus with the text.
func menuAction(t *testing.T, tw *testWindow, text string) *qt.QAction {
	t.Helper()
	for _, m := range []*qt.QMenu{tw.file, tw.help} {
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
	mainthread.Wait(func() {
		for text, want := range map[string]string{"&Settings…": "Ctrl+,", "&Quit": "Ctrl+Q", "&About Prism": "Ctrl+I"} {
			shortcut := menuAction(t, tw, text).Shortcut()
			if got := shortcut.ToString(); got != want {
				t.Errorf("%s shortcut = %q, want %q", text, got, want)
			}
			shortcut.Delete()
		}
	})
}

func TestAboutAction(t *testing.T) {
	tw := newTestWindow(t, nil)
	mainthread.Wait(func() {
		menuAction(t, tw, "&About Prism").Trigger()
		var about *qt.QWidget
		for _, w := range qt.QApplication_TopLevelWidgets() {
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

func TestAbout(t *testing.T) {
	mainthread.Wait(func() {
		d := newAboutDialog(nil)
		defer d.DeleteLater()
		licences := licenceText()
		for _, want := range []string{qt.QLibraryInfo_Version().ToString(), "LGPLv3", "miqt", "Solarized"} {
			if !strings.Contains(licences, want) {
				t.Errorf("the licences do not mention %s", want)
			}
		}
		if !strings.Contains(aboutText(), "GNU General Public License") {
			t.Error("the about box does not give the program's licence")
		}
		d.Layout().Activate()
		if need := d.Layout().HeightForWidth(d.Width()); d.Height() < need {
			t.Errorf("the about box is %d high, and needs %d", d.Height(), need)
		}
	})
}

func TestQuit(t *testing.T) {
	tw := newTestWindow(t, nil)
	mainthread.Wait(func() {
		var quit *qt.QAction
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
	mainthread.Wait(func() {
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
	mainthread.Wait(func() {
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
		mainthread.Wait(tw.sampleCPU)
	}
	mainthread.Wait(func() {
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
	mainthread.Wait(tw.sampleCPU)
	tw.source.set(func(f *fakeSource) {
		f.cpu = stats.CPU{All: stats.CPUTimes{Busy: 1, Idle: 3}, Cores: []stats.CPUTimes{{}, {}}}
	})
	tw.advance(time.Second)
	mainthread.Wait(func() {
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
	mainthread.Wait(func() {
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
	mainthread.Wait(func() {
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
	mainthread.Wait(func() {
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
		if g.top != 16*gib || g.labels[0] != "0 bytes" || g.labels[len(g.labels)-1] != "16.0 GiB" {
			t.Errorf("memory graph top %v, labels %q", g.top, g.labels)
		}
		if p, _ := tw.memory.swap.Last(); p.V != 0 {
			t.Errorf("swap = %v, want 0", p.V)
		}
	})
}

func TestSampleNetwork(t *testing.T) {
	tw := newTestWindow(t, nil)
	tw.source.set(func(f *fakeSource) {
		f.network = map[string]stats.NetCounters{"lo": {Received: 0, Sent: 0}, "eth0": {Received: 1000, Sent: 0}}
	})
	mainthread.Wait(tw.sampleNetwork)

	// The colours of the adapters are given and saved at once.
	saved := settings.Load(tw.path).Colours.Network
	if len(saved) != 2 || saved["eth0"] == saved["lo"] {
		t.Errorf("saved colours = %v, want one for each adapter", saved)
	}
	mainthread.Wait(func() {
		if p := tw.network.adapters["eth0"].received.Points(); len(p) != 2 || p[0].V != 0 || p[1].V != 0 {
			t.Errorf("an adapter is not a line of 0 after a single reading: %+v", p)
		}
	})

	tw.source.set(func(f *fakeSource) {
		f.network = map[string]stats.NetCounters{"lo": {Received: 0, Sent: 0}, "eth0": {Received: 3 * 1024 * 1024, Sent: 2048}}
	})
	tw.advance(2 * time.Second)
	mainthread.Wait(func() {
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
	mainthread.Wait(func() {
		tw.sampleNetwork()
		if len(tw.network.legend.entries) != 1 {
			t.Errorf("legend = %+v, want only lo", tw.network.legend.entries)
		}
		if _, ok := tw.network.adapters["eth0"]; !ok {
			t.Error("eth0 was forgotten while its line is on the graphs")
		}
	})
	tw.advance(2 * time.Minute)
	mainthread.Wait(func() {
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
	mainthread.Wait(tw.sampleNetwork)
	tw.source.set(func(f *fakeSource) {
		f.network = map[string]stats.NetCounters{"lo": {Received: 1 << 30}, "eth0": {Received: 1024}}
	})
	tw.advance(time.Second)
	mainthread.Wait(func() {
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
		if want := "↓ 8.2 kb/s (1.0 kB)   ↑ 0 bits/s (0 bytes)"; tw.network.legend.entries[0].value != want {
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
	mainthread.Wait(func() {
		tw.sampleMemory()
		tw.Resize(800, 750)
		s := tw.config
		s.Width, s.Height = 1, 1
		s.Theme = settings.ThemeDark
		s.Intervals = settings.Intervals{CPU: 0.5, Memory: 90, Network: 3600}
		s.Colours.Memory = "#010203"
		tw.applySettings(s)

		if tw.cpu.timer.Interval() != 500 || tw.memory.timer.Interval() != 90_000 || tw.network.timer.Interval() != 3_600_000 {
			t.Errorf("intervals = %d, %d, %d", tw.cpu.timer.Interval(), tw.memory.timer.Interval(), tw.network.timer.Interval())
		}
		if tw.memory.graph.span != 3*time.Minute {
			t.Errorf("memory span = %v, want 3m", tw.memory.graph.span)
		}
		if tw.memory.legend.entries[0].colour != "#010203" {
			t.Errorf("memory colour = %q", tw.memory.legend.entries[0].colour)
		}
		palette := qt.QGuiApplication_Palette()
		if c := palette.ColorWithCr(qt.QPalette__Window).NameWithFormat(qt.QColor__HexRgb); c != "#2d2d2d" {
			t.Errorf("window colour = %s, want the dark theme's", c)
		}
		palette.Delete()
	})
	s := settings.Load(tw.path)
	if s.Theme != settings.ThemeDark || s.Intervals.Memory != 90 || s.Colours.Memory != "#010203" {
		t.Errorf("saved = %+v", s)
	}
	if s.Width != 800 || s.Height != 750 {
		t.Errorf("saved size = %dx%d, want the window's", s.Width, s.Height)
	}
}

func TestEditSettings(t *testing.T) {
	tw := newTestWindow(t, nil)
	mainthread.Wait(func() {
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
	if s := usedOf(chart.Units{}, 512, 2048); s != "512 bytes (25.0%)" {
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
	t.Cleanup(func() { mainthread.Wait(func() { applyTheme("") }) })
	mainthread.Wait(func() {
		window := func() string {
			p := qt.QGuiApplication_Palette()
			defer p.Delete()
			return p.ColorWithCr(qt.QPalette__Window).NameWithFormat(qt.QColor__HexRgb)
		}
		applyTheme(settings.ThemeLight)
		if c := window(); c != "#efefef" {
			t.Errorf("light window = %s", c)
		}
		if s := qt.QApplication_Style().Name(); s != "fusion" {
			t.Errorf("light style = %s", s)
		}
		applyTheme(settings.ThemeDark)
		if c := window(); c != "#2d2d2d" {
			t.Errorf("dark window = %s", c)
		}
		applyTheme("")
		if s := qt.QApplication_Style().Name(); s != systemStyle {
			t.Errorf("system style = %s, want %s", s, systemStyle)
		}
	})
}

func TestMakePalette(t *testing.T) {
	mainthread.Wait(func() {
		p := makePalette(darkColours)
		defer p.Delete()
		if c := p.Color(qt.QPalette__Disabled, qt.QPalette__Text).NameWithFormat(qt.QColor__HexRgb); c != "#7a7a7a" {
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
	mainthread.Wait(func() {
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
	mainthread.Wait(func() {
		if tw.cpu.total.entries[0].colour != "#444444" || tw.network.legend.entries[0].colour != "#333333" {
			t.Error("the legends do not show the colours set")
		}
	})
}

func TestNetworkTitles(t *testing.T) {
	tw := newTestWindow(t, nil)
	mainthread.Wait(func() {
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
	mainthread.Wait(func() {
		var action *qt.QAction
		for _, a := range tw.file.Actions() {
			if a.Text() == "&Settings…" {
				action = a
			}
		}
		action.Trigger()
		var dialog *qt.QWidget
		for _, w := range qt.QApplication_TopLevelWidgets() {
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
	mainthread.Wait(func() {
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
	mainthread.Wait(func() {
		// A theme the settings choose is in effect from the start.
		if style := qt.QApplication_Style().Name(); !strings.EqualFold(style, "fusion") {
			t.Errorf("style = %q, want Fusion", style)
		}
		tw.Hide()
		tw.Start()
		if !tw.IsVisible() || tw.IsMaximized() {
			t.Error("the window is not shown as it was, unmaximised")
		}
		for _, timer := range []*qt.QTimer{tw.cpu.timer, tw.memory.timer, tw.network.timer} {
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
	mainthread.Wait(func() {
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

func TestSettingsListEveryCore(t *testing.T) {
	for _, cores := range []int{1, 4, 16, 64} {
		tw := newTestWindow(t, nil)
		tw.source.set(func(f *fakeSource) { f.cpu = stats.CPU{Cores: make([]stats.CPUTimes, cores)} })
		mainthread.Wait(func() {
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
	mainthread.Wait(func() {
		// A computer with four cores, of settings from one with more.
		d := newSettingsDialog(nil, current, 4, nil, func(s settings.Settings) { got = &s })
		d.cores[0].set("#aaaaaa")
		d.box.Button(qt.QDialogButtonBox__Ok).Click()
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
	mainthread.Wait(tw.sampleCPU)

	// A processor brought online is drawn across the graph at 0 from its
	// first reading, as those there from the start were.
	tw.source.set(func(f *fakeSource) {
		f.cpu = stats.CPU{All: stats.CPUTimes{Busy: 30, Idle: 20}, Cores: []stats.CPUTimes{{Busy: 20, Idle: 20}, {Busy: 10, Idle: 0}}}
	})
	tw.advance(2 * time.Second)
	mainthread.Wait(func() {
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
	mainthread.Wait(func() {
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
	mainthread.Wait(func() {
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
	mainthread.Wait(func() {
		w := qt.NewQWidget2()
		defer w.Delete()
		for size, want := range map[float64]float64{8: 7, 12: 9, 20: 15} {
			f := qt.NewQFont()
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
func labels(o *qt.QObject) []*qt.QLabel {
	var found []*qt.QLabel
	for _, child := range o.Children() {
		if child.Inherits("QLabel") {
			found = append(found, qt.UnsafeNewQLabel(child.UnsafePointer()))
		}
		found = append(found, labels(child)...)
	}
	return found
}

func TestSettingsIntervalNote(t *testing.T) {
	// The note on the intervals says what range the boxes take.
	mainthread.Wait(func() {
		d := newSettingsDialog(nil, settings.Defaults(), 0, nil, func(settings.Settings) {})
		defer d.Reject()
		found := false
		for _, label := range labels(d.QObject) {
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
	mainthread.Wait(func() {
		tw.SetWindowState(qt.WindowMaximized | qt.WindowMinimized)
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
