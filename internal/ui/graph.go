package ui

import (
	"runtime"
	"time"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/prism/internal/chart"
	"github.com/nomad-software/prism/internal/history"
)

// line is a line of a graph: its colour, as the settings write it, and its
// values.
type line struct {
	colour string
	points []history.Point
}

// graph draws lines of values over time on a grid, with the values of the
// lines of the grid up its right side and how long ago they are along its
// bottom, as GNOME's System Monitor does.
type graph struct {
	*qt.QWidget
	lines []line
	// now is the time at its right edge, and span the time across it.
	now  time.Time
	span time.Duration
	// top is the value at its top; its bottom is 0.
	top float64
	// labels are those of the lines of the grid across it, bottom first.
	labels []string
	// grid is the colour of the grid.
	grid string
	// title, where it is set, is written in the top left corner of the
	// grid, saying what the graph is of, as the network graphs have.
	title string
}

// newGraph returns an empty graph.
func newGraph() *graph {
	g := &graph{QWidget: qt.NewQWidget2(), span: history.MinSpan, top: 100, labels: chart.PercentLabels()}
	g.SetMinimumHeight(110)
	g.SetSizePolicy2(qt.QSizePolicy__Expanding, qt.QSizePolicy__Expanding)
	g.OnPaintEvent(func(_ func(*qt.QPaintEvent), _ *qt.QPaintEvent) { g.paint() })
	return g
}

// show has the graph draw the lines, as they are at the time, up to the top
// with the labels.
func (g *graph) show(now time.Time, span time.Duration, top float64, labels []string, lines []line, grid string) {
	g.now, g.span, g.top, g.labels, g.lines, g.grid = now, span, top, labels, lines, grid
	g.Update()
}

// labelGap is the space between the grid of a graph and its labels, and
// lineWidth how thick its lines are, in pixels.
const (
	labelGap  = 6
	lineWidth = 1.5
)

// titleInset is how far the title of a graph is from the top and the left
// of its grid, in pixels, and titleOpacity how opaque it is: fainter than
// the labels, so that it does not draw the eye from the lines.
const (
	titleInset   = 10
	titleOpacity = 0.2
)

// labelOpacity is how opaque the labels of a graph are.
const labelOpacity = 0.5

// plotRect returns where the grid goes in a graph of the size, with the
// font metrics of its labels: right of them is the widest label, and below
// it a line of them.
func plotRect(width, height int, labels []string, fm *qt.QFontMetrics) chart.Rect {
	widest := 0
	for _, l := range labels {
		widest = max(widest, fm.HorizontalAdvance(l))
	}
	// Half a line is left above, for the top label to be centred on the
	// top of the grid.
	top := float64(fm.Height()) / 2
	return chart.Rect{
		X: 0.5,
		Y: top + 0.5,
		W: max(float64(width-widest-labelGap)-1, 1),
		H: max(float64(height-fm.Height())-top-labelGap/2-1, 1),
	}
}

// paint draws the graph.
func (g *graph) paint() {
	p := qt.NewQPainter2(g.QPaintDevice)
	defer p.Delete()
	defer p.End()

	font := smallFont(g.QWidget)
	defer font.Delete()
	p.SetFont(font)
	fm := qt.NewQFontMetrics(font)
	defer fm.Delete()
	r := plotRect(g.Width(), g.Height(), g.labels, fm)

	palette := g.Palette()
	defer runtime.KeepAlive(palette)
	base := palette.ColorWithCr(qt.QPalette__Base)
	text := palette.ColorWithCr(qt.QPalette__WindowText)
	plot := qt.NewQRectF4(r.X, r.Y, r.W, r.H)
	defer plot.Delete()
	p.FillRect4(plot, base)

	grid := qt.NewQColor6(g.grid)
	defer grid.Delete()
	gridPen := qt.NewQPen3(grid)
	defer gridPen.Delete()
	gridPen.SetCosmetic(true)
	p.SetPenWithPen(gridPen)
	for i := range chart.Rows + 1 {
		y := r.Y + r.H*float64(i)/chart.Rows
		drawLine(p, r.X, y, r.X+r.W, y)
	}
	for i := range chart.Columns + 1 {
		x := r.X + r.W*float64(i)/chart.Columns
		drawLine(p, x, r.Y, x, r.Y+r.H)
	}

	// The labels up the right, each centred on its line, and along the
	// bottom, each beginning at its line.
	p.SetPen(text)
	p.SetOpacity(labelOpacity)
	lineHeight := float64(fm.Height())
	for i, label := range g.labels {
		y := r.Y + r.H - r.H*float64(i)/float64(max(len(g.labels)-1, 1))
		drawText(p, r.X+r.W+labelGap, y-lineHeight/2, float64(fm.HorizontalAdvance(label))+1, lineHeight, int(qt.AlignLeft|qt.AlignVCenter), label)
	}
	for i, label := range chart.TimeLabels(g.span) {
		x := r.X + r.W*float64(i)/chart.Columns
		drawText(p, x+3, r.Y+r.H+labelGap/2, r.W/chart.Columns, lineHeight, int(qt.AlignLeft|qt.AlignTop), label)
	}
	p.SetOpacity(1)

	p.SetRenderHint(qt.QPainter__Antialiasing)
	// The title is in the font of the labels, under the lines.
	if g.title != "" {
		p.SetPen(text)
		p.SetOpacity(titleOpacity)
		drawText(p, r.X+titleInset, r.Y+titleInset, max(r.W-2*titleInset, 0), lineHeight, int(qt.AlignLeft|qt.AlignTop), g.title)
		p.SetOpacity(1)
	}
	p.SetClipRect(plot)
	for _, l := range g.lines {
		g.drawSeries(p, l, r)
	}
}

// drawSeries draws a line of the graph in the rectangle of its grid, as a
// curve through its points.
func (g *graph) drawSeries(p *qt.QPainter, l line, r chart.Rect) {
	if len(l.points) < 2 {
		return
	}
	colour := qt.NewQColor6(l.colour)
	defer colour.Delete()
	pen := qt.NewQPen3(colour)
	defer pen.Delete()
	pen.SetWidthF(lineWidth)
	p.SetPenWithPen(pen)
	p.SetBrushWithStyle(qt.NoBrush)

	placed := chart.Place(l.points, g.now, g.span, g.top, r)
	path := qt.NewQPainterPath()
	defer path.Delete()
	start := qt.NewQPointF3(placed[0].X, placed[0].Y)
	path.MoveTo(start)
	start.Delete()
	for i := 1; i < len(placed); i++ {
		c1, c2 := chart.Controls(placed[i-1], placed[i])
		a, b, end := qt.NewQPointF3(c1.X, c1.Y), qt.NewQPointF3(c2.X, c2.Y), qt.NewQPointF3(placed[i].X, placed[i].Y)
		path.CubicTo(a, b, end)
		a.Delete()
		b.Delete()
		end.Delete()
	}
	p.DrawPath(path)
}

// drawLine draws a straight line.
func drawLine(p *qt.QPainter, x1, y1, x2, y2 float64) {
	from, to := qt.NewQPointF3(x1, y1), qt.NewQPointF3(x2, y2)
	p.DrawLine4(from, to)
	from.Delete()
	to.Delete()
}

// drawText draws text in a rectangle, aligned by the flags.
func drawText(p *qt.QPainter, x, y, w, h float64, flags int, text string) {
	r := qt.NewQRectF4(x, y, w, h)
	p.DrawText5(r, flags, text)
	r.Delete()
}

// smallFont returns the font of the widget, made smaller, which the labels
// of graphs are written in.
func smallFont(w *qt.QWidget) *qt.QFont {
	f := qt.NewQFont5(w.Font())
	if size := f.PointSizeF(); size > 0 {
		f.SetPointSizeF(max(size*0.75, 7))
	}
	return f
}
