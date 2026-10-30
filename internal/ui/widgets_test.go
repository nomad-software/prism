package ui

import (
	"fmt"
	"maps"
	"math"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/prism/internal/chart"
	"github.com/nomad-software/prism/internal/history"
	"github.com/nomad-software/prism/internal/settings"
	"github.com/nomad-software/prism/internal/stats"
)

// render draws the widget into an image of its size.
func render(w *qt.QWidget) *qt.QImage {
	img := qt.NewQImage3(w.Width(), w.Height(), qt.QImage__Format_ARGB32)
	img.Fill(0)
	w.Render(img.QPaintDevice)
	return img
}

// countColour counts the pixels of the image that are the colour, as
// #rrggbb, or near it, as the edges of what is drawn smoothly are.
func countColour(img *qt.QImage, colour string) int {
	want := qt.NewQColor6(colour)
	defer want.Delete()
	r, g, b := want.Red(), want.Green(), want.Blue()
	near := func(got, want int) bool { return got-want < 80 && want-got < 80 }
	n := 0
	for y := range img.Height() {
		for x := range img.Width() {
			// The pixel is 0xAARRGGBB.
			p := int(img.Pixel(x, y))
			if near(p>>16&0xff, r) && near(p>>8&0xff, g) && near(p&0xff, b) {
				n++
			}
		}
	}
	return n
}

func TestGraphPaint(t *testing.T) {
	mainthread.Wait(func() {
		g := newGraph()
		defer g.Delete()
		g.Resize(400, 160)
		now := time.Unix(1_700_000_000, 0)
		var s history.Series
		for i := range 31 {
			s.Add(now.Add(time.Duration(i-30)*2*time.Second), 50)
		}
		// A line of one point is not drawn, and does not fail.
		var single history.Series
		single.Add(now, 10)
		g.show(now, time.Minute, 100, chart.PercentLabels(), []line{{"#ff0000", s.Points(), false}, {"#00ff00", single.Points(), false}}, "#0000ff")
		img := render(g.QWidget)
		defer img.Delete()
		if n := countColour(img, "#ff0000"); n < 300 {
			t.Errorf("the line has %d pixels, want it across the graph", n)
		}
		if n := countColour(img, "#00ff00"); n != 0 {
			t.Errorf("a line of one point has %d pixels", n)
		}
		if n := countColour(img, "#0000ff"); n < 1000 {
			t.Errorf("the grid has %d pixels", n)
		}
	})
}

func TestGraphHold(t *testing.T) {
	mainthread.Wait(func() {
		g := newGraph()
		defer g.Delete()
		g.Resize(400, 160)
		now := time.Unix(1_700_000_000, 0)
		var s history.Series
		s.Add(now.Add(-time.Minute), 50)
		s.Add(now, 50)
		points := s.Points()
		var other history.Series
		other.Add(now.Add(-time.Minute), 20)
		other.Add(now, 20)
		// The graph moves on half a minute: the line that holds goes on to
		// its right edge, and the one that does not stops half way.
		g.show(now, time.Minute, 100, chart.PercentLabels(), []line{{"#ff0000", points, true}, {"#00ff00", other.Points(), false}}, "#0000ff")
		g.advance(now.Add(30 * time.Second))
		img := render(g.QWidget)
		defer img.Delete()
		right := func(colour string) int {
			want := qt.NewQColor6(colour)
			defer want.Delete()
			// The edges of a line drawn smoothly are near its colour.
			near := func(got, want int) bool { return got-want < 80 && want-got < 80 }
			last := -1
			for x := range img.Width() {
				for y := range img.Height() {
					if p := int(img.Pixel(x, y)); near(p>>16&0xff, want.Red()) && near(p>>8&0xff, want.Green()) && near(p&0xff, want.Blue()) {
						last = x
					}
				}
			}
			return last
		}
		font := smallFont(g.QWidget)
		defer font.Delete()
		fm := qt.NewQFontMetrics(font)
		defer fm.Delete()
		r := plotRect(g.Width(), g.Height(), g.labels, fm)
		if x := right("#ff0000"); float64(x) < r.X+r.W-2 {
			t.Errorf("the held line ends at %d, want the right edge, %v", x, r.X+r.W)
		}
		if x, mid := right("#00ff00"), r.X+r.W/2; float64(x) > mid+2 || float64(x) < mid-2 {
			t.Errorf("the line that does not hold ends at %d, want %v", x, mid)
		}
		// The series is as it was.
		if len(s.Points()) != 2 || cap(points) != cap(s.Points()) {
			t.Error("drawing changed the points of the series")
		}
	})
}

// TestGraphKept checks that a graph draws its background and makes its
// curves again only when what they show changes, and not as it moves along.
func TestGraphKept(t *testing.T) {
	mainthread.Wait(func() {
		g := newGraph()
		defer g.Delete()
		g.Resize(400, 160)
		now := time.Unix(1_700_000_000, 0)
		var s history.Series
		s.Add(now.Add(-time.Minute), 20)
		s.Add(now, 60)
		lines := []line{{"#ff0000", s.Points(), true}}
		g.show(now, time.Minute, 100, chart.PercentLabels(), lines, "#0000ff")
		img := render(g.QWidget)
		img.Delete()
		background, layer, pen := g.background, g.layer, g.curves[0].pen

		// Moving along keeps both, and draws the line where it now is.
		g.advance(now.Add(30 * time.Second))
		img = render(g.QWidget)
		if g.background != background || g.layer != layer || g.curves[0].pen != pen {
			t.Error("moving along drew the background or stroked the lines again")
		}
		if !g.drawn.Equal(now.Add(30 * time.Second)) {
			t.Errorf("drawn at %v, want the time moved to", g.drawn)
		}
		if n := countColour(img, "#ff0000"); n < 150 {
			t.Errorf("the line moved along has %d pixels", n)
		}
		img.Delete()

		// A reading that goes on from the lines keeps both; one with another
		// colour strokes them again, but keeps the background; a new colour
		// of the grid draws it again.
		s.Add(now.Add(30*time.Second), 40)
		lines = []line{{"#ff0000", s.Points(), true}}
		g.show(now.Add(30*time.Second), time.Minute, 100, chart.PercentLabels(), lines, "#0000ff")
		render(g.QWidget).Delete()
		if g.background != background || g.layer != layer || g.curves[0].pen != pen {
			t.Error("a reading stroked the lines again, or drew the background again")
		}
		lines = []line{{"#00ff00", s.Points(), true}}
		g.show(now.Add(30*time.Second), time.Minute, 100, chart.PercentLabels(), lines, "#0000ff")
		render(g.QWidget).Delete()
		if g.background != background || g.curves[0].pen == pen {
			t.Error("a new colour did not stroke the lines again, or drew the background again")
		}
		g.show(now, time.Minute, 100, chart.PercentLabels(), lines, "#00ffff")
		img = render(g.QWidget)
		if g.background == background || countColour(img, "#00ffff") < 1000 {
			t.Error("the background was not drawn again in the grid's new colour")
		}
		img.Delete()
		// So does a new size, which makes the curves again to fit it.
		background, layer, pen = g.background, g.layer, g.curves[0].pen
		g.Resize(500, 200)
		render(g.QWidget).Delete()
		if w, want := g.layer.Width(), int(500*g.look.ratio+0.5); w != want {
			t.Errorf("the layer is %d wide, want the graph's %d", w, want)
		}
		if g.background == background || g.layer == layer || g.curves[0].pen == pen {
			t.Error("a new size did not draw the background and stroke the lines again")
		}
	})
}

// TestGraphExtends checks that lines stroked a reading at a time look as they
// do stroked whole, and that what cannot go on from them strokes them whole.
func TestGraphExtends(t *testing.T) {
	mainthread.Wait(func() {
		start := time.Unix(1_700_000_000, 0)
		value := func(i, n int) float64 { return 50 + 45*math.Sin(float64(i*(n+3))*0.7) }
		series := [3]history.Series{}
		lines := func() []line {
			return []line{
				{"#ff0000", series[0].Points(), true},
				// Translucent, as a colour can be, where a join would show most.
				{"#800000ff", series[1].Points(), true},
				{"#00aa00", series[2].Points(), true},
			}
		}
		labels := chart.PercentLabels()
		g := newGraph()
		defer g.Delete()
		g.Resize(600, 200)
		var pen *qt.QPen
		var at time.Time
		for i := range 90 {
			// Not a whole number of pixels from one reading to the next.
			at = start.Add(time.Duration(i) * 1037 * time.Millisecond)
			for n := range series {
				series[n].Fill(at, time.Minute, 0)
				series[n].Add(at, value(i, n))
				series[n].Trim(at.Add(-time.Minute))
			}
			g.show(at, time.Minute, 100, labels, lines(), "#0000ff")
			render(g.QWidget).Delete()
			if i == 0 {
				pen = g.curves[0].pen
			}
		}
		if g.curves[0].pen != pen {
			t.Fatal("the readings stroked the lines whole")
		}
		// Both layers are of the time the first is at, which can be part of a
		// pixel from the last reading.
		whole := newGraph()
		defer whole.Delete()
		whole.Resize(600, 200)
		whole.show(g.curvesAt, time.Minute, 100, labels, lines(), "#0000ff")
		render(whole.QWidget).Delete()
		extended, stroked := g.layer.ToImage(), whole.layer.ToImage()
		defer extended.Delete()
		defer stroked.Delete()
		worst, differing := 0, 0
		for y := range stroked.Height() {
			for x := range stroked.Width() {
				a, b := int(extended.Pixel(x, y)), int(stroked.Pixel(x, y))
				for _, shift := range []int{0, 8, 16, 24} {
					d := a>>shift&0xff - b>>shift&0xff
					worst = max(worst, max(d, -d))
					if d != 0 {
						differing++
					}
				}
			}
		}
		// Only a stray pixel may differ, and hardly, the more so the smaller
		// they are.
		if differing > 32 || worst > 32 {
			t.Errorf("the lines stroked a reading at a time differ from those stroked whole by up to %d, in %d channels", worst, differing)
		}

		// A new scale cannot go on from them.
		g.show(at, time.Minute, 50, labels, lines(), "#0000ff")
		render(g.QWidget).Delete()
		if g.curves[0].pen == pen {
			t.Error("a new scale did not stroke the lines whole")
		}
	})
}

// TestGraphAdvance checks that a graph that is shown is drawn again as it
// moves along only once its lines have moved by a pixel, and one that is not
// shown is not drawn.
func TestGraphAdvance(t *testing.T) {
	mainthread.Wait(func() {
		g := newGraph()
		defer g.Delete()
		g.Resize(400, 160)
		now := time.Unix(1_700_000_000, 0)
		g.show(now, time.Minute, 100, chart.PercentLabels(), nil, "#0000ff")
		g.advance(now.Add(time.Hour))
		qt.QCoreApplication_ProcessEvents()
		if !g.drawn.IsZero() {
			t.Error("a graph that is not shown was drawn")
		}
		g.Show()
		qt.QCoreApplication_ProcessEvents()
		drawn := g.drawn
		if drawn.IsZero() {
			t.Fatal("the graph was not drawn when shown")
		}
		g.advance(drawn.Add(g.pixelTime() / 2))
		qt.QCoreApplication_ProcessEvents()
		if !g.drawn.Equal(drawn) {
			t.Error("the graph was drawn again before its lines moved by a pixel")
		}
		g.advance(drawn.Add(g.pixelTime()))
		qt.QCoreApplication_ProcessEvents()
		if !g.drawn.Equal(drawn.Add(g.pixelTime())) {
			t.Error("the graph was not drawn again once its lines moved by a pixel")
		}
	})
}

func TestPlotRect(t *testing.T) {
	mainthread.Wait(func() {
		font := qt.NewQFont()
		defer font.Delete()
		fm := qt.NewQFontMetrics(font)
		defer fm.Delete()
		labels := chart.PercentLabels()
		r := plotRect(400, 200, labels, fm)
		widest := fm.HorizontalAdvance("100 %")
		if r.X+r.W+labelGap+float64(widest) > 400 {
			t.Errorf("the labels do not fit right of %+v", r)
		}
		if r.Y+r.H+float64(fm.Height()) > 200 {
			t.Errorf("the labels do not fit below %+v", r)
		}
		if r.Y < float64(fm.Height())/2 {
			t.Errorf("the top label does not fit above %+v", r)
		}
		// A graph too small for its labels still has a grid.
		r = plotRect(1, 1, labels, fm)
		if r.W < 1 || r.H < 1 {
			t.Errorf("a tiny graph's grid is %+v", r)
		}
	})
}

func TestLegendLayout(t *testing.T) {
	mainthread.Wait(func() {
		l := newLegend(false)
		defer l.Delete()
		// A widget that is not shown is not told it was resized.
		l.Show()
		var entries []entry
		for i := range 8 {
			entries = append(entries, entry{settings.DefaultCore(i), "CPU" + string(rune('1'+i)), "100.0%", nil})
		}
		l.Resize(4*l.entryWidthFor(entries)+3*columnGap, 10)
		l.show(entries)
		if g := l.grid(); g.Columns != 4 || g.Rows != 2 {
			t.Errorf("grid = %+v, want 4 by 2", g)
		}
		fm := l.FontMetrics()
		lineHeight := max(swatchHeight, fm.Height())
		if want := 2*lineHeight + rowGap; l.Height() != want {
			t.Errorf("height = %d, want %d", l.Height(), want)
		}

		// Narrower, they wrap onto more rows, and the legend grows.
		l.Resize(2*l.entryWidth()+columnGap, l.Height())
		if g := l.grid(); g.Columns != 2 || g.Rows != 4 {
			t.Errorf("narrow grid = %+v, want 2 by 4", g)
		}
		if want := 4*lineHeight + 3*rowGap; l.Height() != want {
			t.Errorf("narrow height = %d, want %d", l.Height(), want)
		}

		// Columns do not shrink when the values do.
		width := l.entryWidth()
		for i := range entries {
			entries[i].value = "1.0%"
		}
		l.show(entries)
		if l.entryWidth() != width {
			t.Errorf("entry width went from %d to %d", width, l.entryWidth())
		}

		img := render(l.QWidget)
		defer img.Delete()
		if countColour(img, settings.DefaultCore(7)) == 0 {
			t.Error("the last swatch is not drawn")
		}

		l.show(nil)
		if l.Height() != 0 {
			t.Errorf("empty legend height = %d", l.Height())
		}
	})
}

// entryWidthFor returns how wide each of the entries would be.
func (l *legend) entryWidthFor(entries []entry) int {
	l.show(entries)
	return l.entryWidth()
}

func TestLegendSwatches(t *testing.T) {
	mainthread.Wait(func() {
		l := newLegend(true)
		defer l.Delete()
		l.Resize(1000, 10)
		l.show([]entry{
			{"#ff0000", "eth0", "↓ 1.0 KiB/s (3.0 MiB)   ↑ 2.0 KiB/s (1.0 MiB)", nil},
			{"#00ff00", "lo", "↓ 0 B/s (0 B)   ↑ 0 B/s (0 B)", nil},
		})
		fm := l.FontMetrics()
		// Each entry is one line, its value beside its name.
		if want := max(swatchHeight, fm.Height()); l.Height() != want {
			t.Errorf("height = %d, want %d", l.Height(), want)
		}
		if g := l.grid(); g.Columns != 2 || g.Rows != 1 {
			t.Errorf("grid = %+v", g)
		}
		if want := swatchWidth + 2*legendGap + fm.HorizontalAdvance("eth0") + fm.HorizontalAdvance("↓ 0 B/s (0 B)   ↑ 0 B/s (0 B)"); l.entryWidth() < want {
			t.Errorf("entry width = %d, want at least %d", l.entryWidth(), want)
		}
		// The swatches are found where they are drawn.
		x, y := l.swatchPlace(1, l.grid(), fm.Height())
		if i := l.swatchAt(x+1, y+1); i != 1 {
			t.Errorf("swatchAt the second swatch = %d", i)
		}
		if i := l.swatchAt(swatchWidth+legendGap+1, y+1); i != -1 {
			t.Errorf("swatchAt the first name = %d", i)
		}
		// Hovering over a swatch rings it, and leaving it clears that.
		l.hover(1)
		if l.hovered != 1 || l.Cursor().Shape() != qt.PointingHandCursor {
			t.Error("the hovered swatch is not ringed with a hand pointer")
		}
		l.hover(-1)
		if l.hovered != -1 || l.Cursor().Shape() == qt.PointingHandCursor {
			t.Error("the ring is not cleared")
		}
		img := render(l.QWidget)
		defer img.Delete()
		if countColour(img, "#00ff00") == 0 {
			t.Error("the second swatch is not drawn")
		}
	})
}

func TestSettingsDialog(t *testing.T) {
	current := settings.Defaults()
	current.Width = 1234
	current.Intervals = settings.Intervals{CPU: 1, Memory: 5, Network: 3600, Processes: 0.5}
	current.CPUMode = settings.CPUTotal
	current.ProcessSort, current.ProcessOrder = settings.SortMemory, settings.OrderAscending
	current.ProcessFilter, current.ProcessView = settings.ProcessesUser, settings.ViewTree
	current.Theme = settings.ThemeLight
	current.SizeUnits, current.RateUnits = settings.SizeBinary, settings.RateBits
	current.Colours.SetCore(1, "#111111")
	current.Colours.Network = map[string]string{"eth0": "#222222", "wlan0": "#333333", "gone": "#444444"}
	current.HiddenAdapters = []string{"gone", "wlan0"}
	current.AdapterAliases = map[string]string{"eth0": "Wired", "gone": "Old"}

	var got *settings.Settings
	mainthread.Wait(func() {
		d := newSettingsDialog(nil, current, 3, []string{"eth0", "wlan0"}, func(s settings.Settings) { got = &s })
		if d.shown.RowCount() != 2 || d.shown.Item(0, colAdapterName).CheckState() != qt.Checked || d.shown.Item(1, colAdapterName).CheckState() != qt.Unchecked {
			t.Error("the adapters shown are not ticked")
		}
		if d.shown.Item(0, colAdapterAlias).Text() != "Wired" || d.shown.Item(1, colAdapterAlias).Text() != "" {
			t.Error("the aliases are not shown")
		}
		// The table has a heading for each of its three sections, and a
		// row for each colour: grid, memory and swap; all cores and three
		// cores; two adapters.
		if rows := d.colourList.RowCount(); rows != 3+3+4+2 {
			t.Errorf("the colour table has %d rows", rows)
		}
		if d.cpuInterval.Value() != 1 || d.memoryInterval.Value() != 5 || d.networkInterval.Value() != 3600 || d.processesInterval.Value() != 0.5 {
			t.Error("the intervals are not shown")
		}
		if d.cpuInterval.Minimum() != 0.1 || d.cpuInterval.Maximum() != 3600 || d.cpuInterval.Decimals() != 1 {
			t.Errorf("interval range = %v to %v, to %d places", d.cpuInterval.Minimum(), d.cpuInterval.Maximum(), d.cpuInterval.Decimals())
		}
		// It opens big enough for its tabs, so that their rows are not
		// squashed.
		if hint := d.SizeHint(); d.Width() < hint.Width() || d.Height() < hint.Height() {
			t.Errorf("the dialog is %dx%d, smaller than its %dx%d", d.Width(), d.Height(), hint.Width(), hint.Height())
		}
		if currentData(d.cpuMode) != settings.CPUTotal || currentData(d.theme) != settings.ThemeLight ||
			currentData(d.sizeUnits) != settings.SizeBinary || currentData(d.rateUnits) != settings.RateBits {
			t.Error("the mode or theme is not shown")
		}

		if len(d.cores) != 3 || d.cores[0].colour != settings.DefaultCore(0) || d.cores[1].colour != "#111111" {
			t.Errorf("cores = %d", len(d.cores))
		}
		if len(d.adapters) != 2 || d.adapters[1].colour != "#333333" || d.adapters[1].Text() != "#333333" {
			t.Error("the adapters are not shown")
		}

		d.networkInterval.SetValue(7)
		d.processesInterval.SetValue(12.5)
		selectData(d.cpuMode, settings.CPUCores)
		selectData(d.theme, "")
		selectData(d.sizeUnits, settings.SizeDecimal)
		selectData(d.rateUnits, settings.RateBytes)
		d.grid.set("#80102030")
		d.cores[2].set("#555555")
		d.adapters[0].set("#666666")
		// A core set back to its default is not kept as a choice.
		d.cores[1].set(settings.DefaultCore(1))
		d.shown.Item(0, colAdapterName).SetCheckState(qt.Unchecked)
		d.shown.Item(1, colAdapterName).SetCheckState(qt.Checked)
		d.shown.Item(0, colAdapterAlias).SetText("  ")
		d.shown.Item(1, colAdapterAlias).SetText(" Wireless ")

		d.box.Button(qt.QDialogButtonBox__Ok).Click()
	})
	if got == nil {
		t.Fatal("accepted was not called")
	}
	if got.Width != 1234 {
		t.Errorf("Width = %d, want it kept", got.Width)
	}
	if got.Intervals != (settings.Intervals{CPU: 1, Memory: 5, Network: 7, Processes: 12.5}) {
		t.Errorf("Intervals = %+v", got.Intervals)
	}
	// How the processes are shown is not in the dialog, and is kept.
	if got.ProcessSort != settings.SortMemory || got.ProcessOrder != settings.OrderAscending ||
		got.ProcessFilter != settings.ProcessesUser || got.ProcessView != settings.ViewTree {
		t.Errorf("processes %q, %q, %q, %q, want them kept", got.ProcessSort, got.ProcessOrder, got.ProcessFilter, got.ProcessView)
	}
	if got.CPUMode != settings.CPUCores || got.Theme != "" {
		t.Errorf("mode %q, theme %q", got.CPUMode, got.Theme)
	}
	if got.SizeUnits != settings.SizeDecimal || got.RateUnits != settings.RateBytes {
		t.Errorf("units %q, %q", got.SizeUnits, got.RateUnits)
	}
	c := got.Colours
	if c.Grid != "#80102030" {
		t.Errorf("Grid = %q", c.Grid)
	}
	if len(c.Cores) != 3 || c.Cores[0] != "" || c.Cores[1] != "" || c.Cores[2] != "#555555" {
		t.Errorf("Cores = %q", c.Cores)
	}
	if c.Network["eth0"] != "#666666" || c.Network["wlan0"] != "#333333" || c.Network["gone"] != "#444444" {
		t.Errorf("Network = %v", c.Network)
	}
	// An adapter that is not listed keeps its choice.
	if want := []string{"eth0", "gone"}; !slices.Equal(got.HiddenAdapters, want) {
		t.Errorf("HiddenAdapters = %v, want %v", got.HiddenAdapters, want)
	}
	// An alias cleared is gone, and one of an adapter not listed is kept.
	if want := map[string]string{"wlan0": "Wireless", "gone": "Old"}; !maps.Equal(got.AdapterAliases, want) {
		t.Errorf("AdapterAliases = %v, want %v", got.AdapterAliases, want)
	}
}

func TestSettingsDialogUseDefaultColours(t *testing.T) {
	current := settings.Defaults()
	current.Intervals.CPU = 9
	current.Colours.Grid = "#123456"
	current.Colours.SetCore(0, "#111111")
	current.Colours.Network = map[string]string{"eth0": "#222222"}
	current.HiddenAdapters = []string{"eth0"}
	mainthread.Wait(func() {
		d := newSettingsDialog(nil, current, 1, []string{"eth0"}, func(settings.Settings) {})
		defer d.Reject()
		if d.resetColours.Text() != "Use Default Colours" {
			t.Errorf("reset button = %q", d.resetColours.Text())
		}
		d.resetColours.Click()
		if d.grid.colour != settings.DefaultGrid || d.cores[0].colour != settings.DefaultCore(0) {
			t.Error("the colours are not the defaults")
		}
		if d.adapters[0].colour == "#222222" {
			t.Error("the adapter was not given its colour afresh")
		}
		// Only the colours are reset.
		if d.cpuInterval.Value() != 9 || d.shown.Item(0, colAdapterName).CheckState() != qt.Unchecked {
			t.Error("Use Default Colours changed more than the colours")
		}
	})
}

func TestSwatch(t *testing.T) {
	mainthread.Wait(func() {
		s := newSwatch(nil, "Grid")
		defer s.Delete()
		s.set("#80ffffff")
		if s.Text() != "#80ffffff" || s.colour != "#80ffffff" {
			t.Errorf("swatch = %q, %q", s.Text(), s.colour)
		}
		if css := s.StyleSheet(); !strings.Contains(css, "rgba(255, 255, 255, 128)") || !strings.Contains(css, "color: #000000") {
			t.Errorf("style = %q", css)
		}
		if s.Width() != swatchButtonWidth && s.MinimumWidth() != swatchButtonWidth {
			t.Errorf("width = %d", s.MinimumWidth())
		}
	})
}

func TestReadableOn(t *testing.T) {
	mainthread.Wait(func() {
		for colour, want := range map[string]string{"#ffffff": "#000000", "#000000": "#ffffff", "#f6d32d": "#000000", "#1c71d8": "#ffffff"} {
			c := qt.NewQColor6(colour)
			if got := readableOn(c); got != want {
				t.Errorf("readableOn(%s) = %s, want %s", colour, got, want)
			}
			c.Delete()
		}
	})
}

func TestSectionHeadingAndNote(t *testing.T) {
	mainthread.Wait(func() {
		h := sectionHeading("Colours")
		defer h.Delete()
		if !h.Font().Bold() {
			t.Error("a heading is not bold")
		}
		n := note("Something")
		defer n.Delete()
		if !n.WordWrap() || n.IsEnabled() {
			t.Error("a note does not wrap, or is not dimmed")
		}
	})
}

func TestSettingsDialogRestoreDefaults(t *testing.T) {
	current := settings.Defaults()
	current.Width, current.Height = 640, 480
	current.Intervals.CPU = 9
	current.Theme = settings.ThemeDark
	current.ProcessSort, current.ProcessOrder = settings.SortName, settings.OrderAscending
	current.Colours.Grid = "#123456"
	current.Colours.Network = map[string]string{"eth0": "#222222", "wlan0": "#333333"}
	current.AdapterAliases = map[string]string{"eth0": "Wired"}

	var got *settings.Settings
	mainthread.Wait(func() {
		d := newSettingsDialog(nil, current, 1, []string{"eth0", "wlan0"}, func(s settings.Settings) { got = &s })
		d.box.Button(qt.QDialogButtonBox__RestoreDefaults).Click()
		if d.cpuInterval.Value() != settings.DefaultIntervals.CPU || currentData(d.theme) != "" || d.grid.colour != settings.DefaultGrid ||
			d.shown.Item(0, colAdapterAlias).Text() != "" {
			t.Error("the defaults are not shown")
		}
		d.box.Button(qt.QDialogButtonBox__Ok).Click()
	})
	if got == nil {
		t.Fatal("accepted was not called")
	}
	want := settings.Defaults()
	want.Width, want.Height = 640, 480
	want.Colours.Adapter("eth0")
	want.Colours.Adapter("wlan0")
	if got.Intervals != want.Intervals || got.Theme != want.Theme || got.CPUMode != want.CPUMode || got.AdapterAliases != nil {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if got.Colours.Grid != want.Colours.Grid || got.Colours.Network["eth0"] != want.Colours.Network["eth0"] ||
		got.Colours.Network["wlan0"] != want.Colours.Network["wlan0"] {
		t.Errorf("colours = %+v, want %+v", got.Colours, want.Colours)
	}
	if got.Width != 640 {
		t.Errorf("Width = %d, want it kept", got.Width)
	}
}

func TestSettingsDialogCancel(t *testing.T) {
	called := false
	mainthread.Wait(func() {
		d := newSettingsDialog(nil, settings.Defaults(), 0, nil, func(settings.Settings) { called = true })
		if d.adapters != nil {
			t.Error("there are adapter buttons with no adapters")
		}
		d.box.Button(qt.QDialogButtonBox__Cancel).Click()
		if d.IsVisible() {
			t.Error("the dialog is still shown")
		}
	})
	if called {
		t.Error("accepted was called on Cancel")
	}
}

func TestColourName(t *testing.T) {
	mainthread.Wait(func() {
		opaque := qt.NewQColor6("#AbCdEf")
		defer opaque.Delete()
		if n := colourName(opaque); n != "#abcdef" {
			t.Errorf("opaque = %q", n)
		}
		clear := qt.NewQColor6("#80abcdef")
		defer clear.Delete()
		if n := colourName(clear); n != "#80abcdef" {
			t.Errorf("translucent = %q", n)
		}
	})
}

func TestSelectData(t *testing.T) {
	mainthread.Wait(func() {
		box := qt.NewQComboBox2()
		defer box.Delete()
		addItem(box, "A", "a")
		addItem(box, "B", "b")
		selectData(box, "b")
		if currentData(box) != "b" {
			t.Errorf("current = %q", currentData(box))
		}
		// Data that is not there leaves the current item as it is.
		selectData(box, "c")
		if currentData(box) != "b" {
			t.Errorf("current after missing = %q", currentData(box))
		}
	})
}

// TestScreenshot draws the window, with a few minutes of made up readings,
// into the PNG file PRISM_SCREENSHOT names, as make screenshot has
// it. It does nothing without it.
func TestScreenshot(t *testing.T) {
	path := os.Getenv("PRISM_SCREENSHOT")
	if path == "" {
		t.Skip("PRISM_SCREENSHOT is not set")
	}
	tw := newTestWindow(t, func(s *settings.Settings) { s.Theme = settings.ThemeDark })
	const cores = 8
	var busy, idle [cores]uint64
	var received, sent uint64
	for step := range 40 {
		tw.source.set(func(f *fakeSource) {
			f.cpu = stats.CPU{Cores: make([]stats.CPUTimes, cores)}
			for i := range cores {
				// Each core is busy in a wave of its own.
				b := uint64(20 + (step*7+i*13)%60)
				busy[i] += b
				idle[i] += 100 - b
				f.cpu.Cores[i] = stats.CPUTimes{Busy: busy[i], Idle: idle[i]}
				f.cpu.All.Busy += busy[i]
				f.cpu.All.Idle += idle[i]
			}
			f.memory = stats.Memory{Total: 32 << 30, Used: uint64(14<<30 + step*(20<<20)), SwapTotal: 8 << 30, SwapUsed: 6 << 20}
			received += uint64(200<<10 + (step%9)*(150<<10))
			sent += uint64(20<<10 + (step%5)*(30<<10))
			f.network = map[string]stats.NetCounters{
				"enp5s0":  {Received: received, Sent: sent},
				"wlan0":   {Received: received / 4, Sent: sent / 3},
				"docker0": {Received: uint64(step) * 1000, Sent: uint64(step) * 500},
			}
		})
		mainthread.Wait(func() {
			tw.sampleCPU()
			tw.sampleMemory()
			tw.sampleNetwork()
		})
		tw.advance(2 * time.Second)
	}
	mainthread.Wait(func() {
		tw.Resize(900, 820)
		// The picture is a copy, which its finalizer frees.
		img := tw.Grab()
		if !img.Save(path) {
			t.Errorf("the screenshot could not be saved to %s", path)
		}
	})
}

// stubColour has pickColour, while the test runs, choose the colour, or
// none if it is "", without a dialog. It returns the names and colours it
// was asked to choose for, as "name colour".
func stubColour(t *testing.T, colour string) *[]string {
	t.Helper()
	asked := &[]string{}
	old := pickColour
	pickColour = func(_ *qt.QWidget, name, initial string) (string, bool) {
		*asked = append(*asked, name+" "+initial)
		return colour, colour != ""
	}
	t.Cleanup(func() { pickColour = old })
	return asked
}

// differ counts the pixels of the part of each image, at x, y and as big
// as w by h, that are not the same in both.
func differ(a, b *qt.QImage, x, y, w, h int) int {
	n := 0
	for j := y; j < y+h; j++ {
		for i := x; i < x+w; i++ {
			if a.Pixel(i, j) != b.Pixel(i, j) {
				n++
			}
		}
	}
	return n
}

func TestGraphTitle(t *testing.T) {
	mainthread.Wait(func() {
		images := map[string]*qt.QImage{}
		for _, title := range []string{"", "Download", "Upload"} {
			g := newGraph()
			g.title = title
			g.Resize(400, 160)
			images[title] = render(g.QWidget)
			g.Delete()
		}
		defer func() {
			for _, img := range images {
				img.Delete()
			}
		}()
		// The titles are in the top left corner of the grid, and are not
		// alike.
		const x, y, w, h = 0, 0, 120, 40
		if n := differ(images[""], images["Download"], x, y, w, h); n < 10 {
			t.Errorf("the Download title has %d pixels", n)
		}
		if n := differ(images["Download"], images["Upload"], x, y, w, h); n < 10 {
			t.Error("the titles are drawn alike")
		}
		// Nothing else of the graph is changed.
		if n := differ(images[""], images["Download"], 150, 60, 150, 60); n != 0 {
			t.Errorf("the title changed %d pixels away from the corner", n)
		}
	})
}

func TestLegendClick(t *testing.T) {
	var set []string
	asked := stubColour(t, "#123456")
	mainthread.Wait(func() {
		l := newLegend(false)
		defer l.Delete()
		l.Show()
		l.Resize(1000, 10)
		l.show([]entry{
			{"#ff0000", "CPU1", "1.0%", func(c string) { set = append(set, c) }},
			{"#00ff00", "CPU2", "2.0%", nil},
		})
		fm := l.FontMetrics()
		x0, y0 := l.swatchPlace(0, l.grid(), fm.Height())
		x1, y1 := l.swatchPlace(1, l.grid(), fm.Height())

		if i := l.settableAt(x0+1, y0+1); i != 0 {
			t.Errorf("settableAt the first swatch = %d", i)
		}
		// An entry without a setter is not a shortcut.
		if i := l.settableAt(x1+1, y1+1); i != -1 {
			t.Errorf("settableAt the second swatch = %d", i)
		}
		if l.click(x1+1, y1+1) || l.click(x0+swatchWidth+legendGap+1, y0+1) {
			t.Error("a click off a settable swatch was taken")
		}
		if len(*asked) != 0 {
			t.Errorf("asked for %q", *asked)
		}

		l.hover(0)
		if !l.click(x0+1, y0+1) {
			t.Error("a click on the swatch was not taken")
		}
		if want := []string{"CPU1 #ff0000"}; !slices.Equal(*asked, want) {
			t.Errorf("asked for %q, want %q", *asked, want)
		}
		if want := []string{"#123456"}; !slices.Equal(set, want) {
			t.Errorf("set %q, want %q", set, want)
		}
		if l.hovered != -1 {
			t.Error("the swatch is still ringed while its colour is chosen")
		}
	})
}

func TestLegendClickCancelled(t *testing.T) {
	stubColour(t, "")
	mainthread.Wait(func() {
		l := newLegend(false)
		defer l.Delete()
		l.Show()
		l.Resize(1000, 10)
		called := false
		l.show([]entry{{"#ff0000", "eth0", "↓ 0 B/s (0 B)", func(string) { called = true }}})
		fm := l.FontMetrics()
		x, y := l.swatchPlace(0, l.grid(), fm.Height())
		if !l.click(x+1, y+1) || called {
			t.Error("a colour was set though none was chosen")
		}
	})
}

func TestLegendSwatchPlaces(t *testing.T) {
	colours := []string{"#ff0000", "#00ff00", "#0000ff", "#ffff00", "#ff00ff"}
	mainthread.Wait(func() {
		l := newLegend(false)
		defer l.Delete()
		l.Show()
		var entries []entry
		for i, c := range colours {
			entries = append(entries, entry{c, fmt.Sprintf("CPU%d", i+1), "1.0%", func(string) {}})
		}
		l.show(entries)
		// Two entries a row, so that they wrap into three rows.
		l.Resize(2*l.entryWidth()+columnGap+10, 10)
		img := render(l.QWidget)
		defer img.Delete()
		fm := l.FontMetrics()
		g := l.grid()
		if g.Columns != 2 || g.Rows != 3 {
			t.Fatalf("grid = %+v, want 2 columns of 3 rows", g)
		}
		for i, c := range colours {
			// Each swatch is drawn where a click finds it.
			x, y := l.swatchPlace(i, g, fm.Height())
			cx, cy := x+swatchWidth/2, y+swatchHeight/2
			if got := fmt.Sprintf("#%06x", img.Pixel(int(cx), int(cy))&0xffffff); got != c {
				t.Errorf("swatch %d at %v,%v is %s, want %s", i, cx, cy, got, c)
			}
			if l.swatchAt(cx, cy) != i {
				t.Errorf("swatch %d is not found at its middle", i)
			}
		}
	})
}

func TestLegendRing(t *testing.T) {
	mainthread.Wait(func() {
		l := newLegend(false)
		defer l.Delete()
		l.Show()
		l.Resize(400, 10)
		l.show([]entry{{"#ff0000", "CPU1", "1.0%", func(string) {}}})
		plain := render(l.QWidget)
		defer plain.Delete()
		l.hover(0)
		ringed := render(l.QWidget)
		defer ringed.Delete()
		fm := l.FontMetrics()
		x, y := l.swatchPlace(0, l.grid(), fm.Height())
		if n := differ(plain, ringed, int(x), int(y), swatchWidth, swatchHeight); n < 20 {
			t.Errorf("the ring has %d pixels", n)
		}
		// The ring is inside the swatch, but for the pixel its smoothed edge
		// and the half pixel the swatch can stand at reach into.
		w, h := plain.Width(), plain.Height()
		if n := differ(plain, ringed, 0, 0, w, h) - differ(plain, ringed, int(x)-1, int(y)-1, swatchWidth+2, swatchHeight+2); n != 0 {
			t.Errorf("the ring changed %d pixels outside the swatch", n)
		}
	})
}

func TestSwatchPick(t *testing.T) {
	asked := stubColour(t, "#abcdef")
	mainthread.Wait(func() {
		s := newSwatch(nil, "Grid")
		defer s.Delete()
		s.set("#000000")
		s.Click()
		if s.colour != "#abcdef" || s.Text() != "#abcdef" {
			t.Errorf("swatch = %q, want the colour chosen", s.colour)
		}
		if want := []string{"Grid #000000"}; !slices.Equal(*asked, want) {
			t.Errorf("asked for %q, want %q", *asked, want)
		}
	})
	stubColour(t, "")
	mainthread.Wait(func() {
		s := newSwatch(nil, "Grid")
		defer s.Delete()
		s.set("#000000")
		s.Click()
		if s.colour != "#000000" {
			t.Errorf("swatch = %q after no colour was chosen", s.colour)
		}
	})
}

// sendMouse sends the widget a mouse event of the type, with the button, at
// the place.
func sendMouse(w *qt.QWidget, kind qt.QEvent__Type, button qt.MouseButton, x, y float64) {
	pos := qt.NewQPointF3(x, y)
	defer pos.Delete()
	e := qt.NewQMouseEvent(kind, pos, button, button, qt.NoModifier)
	defer e.Delete()
	qt.QCoreApplication_SendEvent(w.QObject, e.QEvent)
}

func TestLegendMouseEvents(t *testing.T) {
	asked := stubColour(t, "#123456")
	mainthread.Wait(func() {
		l := newLegend(false)
		defer l.Delete()
		l.Show()
		l.Resize(1000, 10)
		l.show([]entry{{"#ff0000", "CPU1", "1.0%", func(string) {}}})
		fm := l.FontMetrics()
		x, y := l.swatchPlace(0, l.grid(), fm.Height())

		sendMouse(l.QWidget, qt.QEvent__MouseMove, qt.NoButton, x+1, y+1)
		if l.hovered != 0 {
			t.Error("moving over the swatch does not ring it")
		}
		leave := qt.NewQEvent(qt.QEvent__Leave)
		defer leave.Delete()
		qt.QCoreApplication_SendEvent(l.QObject, leave)
		if l.hovered != -1 {
			t.Error("leaving the legend does not clear the ring")
		}

		// Only the left button chooses a colour.
		sendMouse(l.QWidget, qt.QEvent__MouseButtonPress, qt.RightButton, x+1, y+1)
		if len(*asked) != 0 {
			t.Error("the right button chose a colour")
		}
		sendMouse(l.QWidget, qt.QEvent__MouseButtonPress, qt.LeftButton, x+1, y+1)
		if len(*asked) != 1 {
			t.Errorf("the left button asked %d times", len(*asked))
		}
	})
}

// answerColourDialog has the colour dialog, once it is shown, choose the
// colour, or be cancelled if it is "", and returns the title it was shown
// with.
func answerColourDialog(colour string) *string {
	title := new(string)
	timer := qt.NewQTimer()
	timer.SetInterval(10)
	timer.OnTimeout(func() {
		w := qt.QApplication_ActiveModalWidget()
		if w == nil {
			return
		}
		timer.Stop()
		*title = w.WindowTitle()
		d := qt.UnsafeNewQColorDialog(w.UnsafePointer())
		if colour == "" {
			d.Reject()
			return
		}
		c := qt.NewQColor6(colour)
		defer c.Delete()
		d.SetCurrentColor(c)
		d.Accept()
	})
	timer.Start2()
	return title
}

func TestPickColour(t *testing.T) {
	mainthread.Wait(func() {
		title := answerColourDialog("#80123456")
		colour, ok := pickColour(nil, "Grid", "#000000")
		if !ok || colour != "#80123456" {
			t.Errorf("pickColour = %q, %v, want the translucent colour chosen", colour, ok)
		}
		if *title != "Colour of Grid" {
			t.Errorf("dialog title = %q", *title)
		}
		answerColourDialog("")
		if colour, ok := pickColour(nil, "Grid", "#000000"); ok {
			t.Errorf("a cancelled dialog chose %q", colour)
		}
	})
}

func TestLegendFaint(t *testing.T) {
	mainthread.Wait(func() {
		images := map[bool]*qt.QImage{}
		var l *legend
		for _, faint := range []bool{false, true} {
			l = newLegend(faint)
			l.Show()
			l.Resize(600, 10)
			l.show([]entry{{"#ff0000", "Memory", "4.0 GiB (25.0%)", nil}})
			images[faint] = render(l.QWidget)
			defer images[faint].Delete()
			defer l.Delete()
		}
		// The names are the same, and the values are not.
		valueX := swatchWidth + legendGap + l.labelWidth + legendGap
		h := images[false].Height()
		if n := differ(images[false], images[true], 0, 0, valueX, h); n != 0 {
			t.Errorf("a faint value changed %d pixels of the name", n)
		}
		if n := differ(images[false], images[true], valueX, 0, l.valueWidth, h); n < 20 {
			t.Errorf("a faint value differs in %d pixels", n)
		}
	})
}

func TestLegendNamesShrink(t *testing.T) {
	mainthread.Wait(func() {
		l := newLegend(true)
		defer l.Delete()
		l.show([]entry{{"#ff0000", "Mozilla VPN", "↓ 0 b/s", nil}, {"#00ff00", "LAN", "↓ 0 b/s", nil}})
		long := l.labelWidth
		// Hiding the adapter with the longer name leaves no gap after the
		// shorter one.
		l.show([]entry{{"#00ff00", "LAN", "↓ 0 b/s", nil}})
		if want := l.FontMetrics().HorizontalAdvance("LAN"); l.labelWidth != want || l.labelWidth >= long {
			t.Errorf("labelWidth = %d, want %d", l.labelWidth, want)
		}
	})
}

func TestLegendFaintValueFollowsName(t *testing.T) {
	mainthread.Wait(func() {
		images := map[string]*qt.QImage{}
		var l *legend
		for _, value := range []string{"↓ 1.0 MB/s", ""} {
			l = newLegend(true)
			defer l.Delete()
			l.Show()
			l.Resize(1000, 10)
			l.show([]entry{{"#ff0000", "LAN", value, nil}, {"#00ff00", "Mozilla VPN", "↓ 1.0 MB/s", nil}})
			images[value] = render(l.QWidget)
			defer images[value].Delete()
		}
		// LAN's value is drawn where a column as wide as the longer name
		// would leave a gap.
		x := swatchWidth + legendGap + l.FontMetrics().HorizontalAdvance("LAN") + legendGap
		w := l.labelWidth + legendGap - (x - swatchWidth - legendGap)
		if n := differ(images["↓ 1.0 MB/s"], images[""], x, 0, w, l.Height()); n == 0 {
			t.Error("there is a gap between LAN and its value")
		}
	})
}
