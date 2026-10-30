package ui

import (
	"math"
	"runtime"
	"slices"
	"strings"
	"time"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/prism/internal/chart"
	"github.com/nomad-software/prism/internal/history"
)

// line is a line of a graph: its colour, as the settings write it, and its
// values. A line that holds is drawn on from its newest value, unchanged, to
// the right edge of the graph, as it is until the next reading puts it right.
type line struct {
	colour string
	points []history.Point
	hold   bool
}

// graph draws lines of values over time on a grid, with the values of the
// lines of the grid up its right side and how long ago they are along its
// bottom.
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
	// drawn is the time that was at its right edge when it was last drawn.
	drawn time.Time
	// background is the grid, filled, with its labels and title, as last
	// drawn, which is drawn from until what it shows, look, changes; plot
	// is where the grid is in it.
	background *qt.QPixmap
	look       look
	plot       chart.Rect
	// layer is the lines, stroked on a clear pixmap as they were at the time
	// curvesAt, to the scale of curvesTop, which is drawn from, moved along,
	// as stroking them is nearly all the time drawing a graph takes. A reading
	// scrolls it and strokes only what is new. curves are what is known of the
	// lines on it, and stale says that the lines have changed since.
	layer     *qt.QPixmap
	curves    []curve
	curvesAt  time.Time
	curvesTop float64
	stale     bool
}

// look is what the background of a graph shows: its size, the scale of its
// grid, its labels and title, and the colours and font they are drawn in.
type look struct {
	width, height int
	ratio         float64
	span          time.Duration
	labels, title string
	grid, font    string
	base, text    uint
}

// curve is what is known of a line of a graph on the layer: its colour and
// pen, and its newest point, newest, which is at last on the layer. A line
// that holds, and has a point, has hold set, and is drawn on from there to the
// right edge.
type curve struct {
	colour string
	pen    *qt.QPen
	hold   bool
	newest history.Point
	last   chart.XY
}

// note keeps what the curve needs of the line, whose points are placed on the
// layer.
func (c *curve) note(l line, placed []chart.XY) {
	n := len(placed)
	c.hold = l.hold && n > 0
	if n > 0 {
		c.newest, c.last = l.points[n-1], placed[n-1]
	}
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
	g.stale = true
	g.Update()
}

// advance moves the right edge of the graph on to the time, so that its
// lines move along between readings. It is drawn again only where it is
// shown and they have moved by a pixel of the screen since it last was, as
// drawing it more often would not move them.
func (g *graph) advance(now time.Time) {
	g.now = now
	if g.IsVisible() && now.Sub(g.drawn) >= g.pixelTime() {
		g.Update()
	}
}

// pixelTime returns how long the lines of the graph take to move along by a
// pixel of the screen, as near as the width of the graph, which is a little
// more than that of its grid, says.
func (g *graph) pixelTime() time.Duration {
	pixels := float64(g.Width()) * g.DevicePixelRatioF()
	return time.Duration(float64(g.span) / max(pixels, 1))
}

// forgetCurves lets the curves made of the lines go, for them and the layer
// to be made again from the lines as they are now.
func (g *graph) forgetCurves() {
	for _, c := range g.curves {
		c.pen.Delete()
	}
	g.curves = nil
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

// paint draws the graph: the background, as it was drawn last unless what
// it shows has changed, and the lines on it, as they were last stroked, moved
// along to now.
func (g *graph) paint() {
	p := qt.NewQPainter2(g.QPaintDevice)
	defer p.Delete()
	defer p.End()

	font := smallFont(g.QWidget)
	defer font.Delete()
	palette := g.Palette()
	defer runtime.KeepAlive(palette)
	base := palette.ColorWithCr(qt.QPalette__Base)
	text := palette.ColorWithCr(qt.QPalette__WindowText)
	want := look{
		width: g.Width(), height: g.Height(), ratio: g.DevicePixelRatioF(),
		span: g.span, labels: strings.Join(g.labels, "\n"), title: g.title,
		grid: g.grid, font: font.Key(), base: base.Rgba(), text: text.Rgba(),
	}
	if g.background == nil || want != g.look {
		g.drawBackground(want, font, base, text)
	}
	p.DrawPixmap9(0, 0, g.background)

	r := g.plot
	if g.curves == nil {
		g.makeCurves(r)
	} else if g.stale {
		g.updateCurves(r)
	}
	g.stale = false
	p.SetRenderHint(qt.QPainter__Antialiasing)
	plot := qt.NewQRectF4(r.X, r.Y, r.W, r.H)
	defer plot.Delete()
	p.SetClipRect(plot)
	g.drawLines(p, r)
	g.drawn = g.now
}

// newLookPixmap returns a clear pixmap of the size and ratio of the look.
func newLookPixmap(l look) *qt.QPixmap {
	pixmap := qt.NewQPixmap2(int(float64(l.width)*l.ratio+0.5), int(float64(l.height)*l.ratio+0.5))
	pixmap.SetDevicePixelRatio(l.ratio)
	clearPixmap(pixmap)
	return pixmap
}

// clearPixmap makes the pixmap clear.
func clearPixmap(pixmap *qt.QPixmap) {
	transparent := qt.NewQColor2(qt.Transparent)
	defer transparent.Delete()
	pixmap.FillWithFillColor(transparent)
}

// drawBackground draws what the background shows into a pixmap of the size
// of the graph: the grid, filled, the labels up its right side and along its
// bottom, and the title in its top left corner. The grid can have moved, and
// the layer is of the same size, so the lines are made again to fit.
func (g *graph) drawBackground(l look, font *qt.QFont, base, text *qt.QColor) {
	if g.background != nil {
		g.background.Delete()
		g.layer.Delete()
	}
	g.background = newLookPixmap(l)
	g.layer = newLookPixmap(l)
	g.forgetCurves()
	g.look = l

	p := qt.NewQPainter2(g.background.QPaintDevice)
	defer p.Delete()
	defer p.End()
	p.SetFont(font)
	fm := qt.NewQFontMetrics(font)
	defer fm.Delete()
	r := plotRect(l.width, l.height, g.labels, fm)
	g.plot = r
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

	// The title is in the font of the labels, under the lines.
	if g.title != "" {
		p.SetRenderHint(qt.QPainter__Antialiasing)
		p.SetOpacity(titleOpacity)
		drawText(p, r.X+titleInset, r.Y+titleInset, max(r.W-2*titleInset, 0), lineHeight, int(qt.AlignLeft|qt.AlignTop), g.title)
	}
}

// makeCurves strokes the whole of every line on the layer, in the rectangle of
// the grid, as they are at the time the graph is at: each a curve through its
// points. They are cut off at the grid when the layer is drawn.
func (g *graph) makeCurves(r chart.Rect) {
	clearPixmap(g.layer)
	p := newLayerPainter(g.layer)
	defer p.Delete()
	defer p.End()
	g.curves = make([]curve, 0, len(g.lines))
	for _, l := range g.lines {
		colour := qt.NewQColor6(l.colour)
		c := curve{colour: l.colour, pen: qt.NewQPen3(colour)}
		colour.Delete()
		c.pen.SetWidthF(lineWidth)
		placed := chart.Place(l.points, g.now, g.span, g.top, r)
		c.note(l, placed)
		strokeLine(p, c.pen, placed)
		g.curves = append(g.curves, c)
	}
	g.curvesAt, g.curvesTop = g.now, g.top
}

// newLayerPainter returns a painter ready to stroke lines on the layer.
func newLayerPainter(layer *qt.QPixmap) *qt.QPainter {
	p := qt.NewQPainter2(layer.QPaintDevice)
	p.SetRenderHint(qt.QPainter__Antialiasing)
	p.SetBrushWithStyle(qt.NoBrush)
	return p
}

// strokeLine strokes a curve through the places, of which there must be two
// for there to be one.
func strokeLine(p *qt.QPainter, pen *qt.QPen, placed []chart.XY) {
	if len(placed) < 2 {
		return
	}
	path := qt.NewQPainterPath()
	defer path.Delete()
	path.MoveTo2(placed[0].X, placed[0].Y)
	for i := 1; i < len(placed); i++ {
		c1, c2 := chart.Controls(placed[i-1], placed[i])
		path.CubicTo2(c1.X, c1.Y, c2.X, c2.Y, placed[i].X, placed[i].Y)
	}
	p.SetPenWithPen(pen)
	p.DrawPath(path)
}

// newSince says what the lines have that the layer has not: whether they have
// points it has not, the oldest time they go on from if they have, and whether
// they are the lines on the layer, gone on with, at all. They are not if the
// scale, their number or a colour changed, or the point a line reached has gone
// from it, or differs, as of another line.
func (g *graph) newSince() (from time.Time, fresh, same bool) {
	if g.top != g.curvesTop || len(g.lines) != len(g.curves) {
		return from, false, false
	}
	for i, l := range g.lines {
		c := g.curves[i]
		if l.colour != c.colour {
			return from, false, false
		}
		reached := slices.Index(l.points, c.newest)
		switch {
		case c.newest.T.IsZero() && len(l.points) > 0, !c.newest.T.IsZero() && reached < 0:
			return from, false, false
		case reached >= 0 && reached < len(l.points)-1 && (!fresh || c.newest.T.Before(from)):
			from, fresh = c.newest.T, true
		}
	}
	return from, fresh, true
}

// updateCurves brings the layer up to the lines as they are now, stroking only
// what is new if they go on from those on it, and all of them if not.
func (g *graph) updateCurves(r chart.Rect) {
	from, fresh, same := g.newSince()
	if !same || g.now.Before(g.curvesAt) || !g.extendCurves(r, from, fresh) {
		g.forgetCurves()
		g.makeCurves(r)
	}
}

// strokeMargin is how far before where a line is stroked on from its path
// starts, so that the end of the path is not seen, in pixels.
const strokeMargin = 2

// extendCurves scrolls the layer along to the time the graph is at, by whole
// pixels of the screen, and clears it from where the lines go on from, a whole
// pixel at or before the time, then strokes what is there again of each line.
// Their paths start before there, and are cut off at it, so that the pixels
// either side are of one line, with no join to be seen. Where it would be
// scrolled right off, it returns false. Lines with nothing new are only noted.
func (g *graph) extendCurves(r chart.Rect, from time.Time, fresh bool) bool {
	if !fresh {
		for i, l := range g.lines {
			g.curves[i].hold = l.hold && !g.curves[i].newest.T.IsZero()
		}
		return true
	}
	ratio := g.look.ratio
	across := r.W * ratio / g.span.Seconds()
	shift := int(math.Round(g.now.Sub(g.curvesAt).Seconds() * across))
	if shift >= g.layer.Width() {
		return false
	}
	if shift > 0 {
		g.layer.Scroll(-shift, 0, 0, 0, g.layer.Width(), g.layer.Height())
		g.curvesAt = g.curvesAt.Add(time.Duration(float64(shift) / across * float64(time.Second)))
	}
	place := func(points []history.Point) []chart.XY {
		return chart.Place(points, g.curvesAt, g.span, g.top, r)
	}
	start := math.Floor(place([]history.Point{{T: from}})[0].X*ratio) / ratio
	rest := qt.NewQRectF4(start, 0, float64(g.look.width)-start, float64(g.look.height))
	defer rest.Delete()

	p := newLayerPainter(g.layer)
	defer p.Delete()
	defer p.End()
	p.SetCompositionMode(qt.QPainter__CompositionMode_Clear)
	transparent := qt.NewQColor2(qt.Transparent)
	defer transparent.Delete()
	p.FillRect4(rest, transparent)
	p.SetCompositionMode(qt.QPainter__CompositionMode_SourceOver)
	p.SetClipRect(rest)
	for i, l := range g.lines {
		placed := place(l.points)
		g.curves[i].note(l, placed)
		first := 0
		for j, at := range placed {
			if at.X <= start-strokeMargin {
				first = j
			}
		}
		strokeLine(p, g.curves[i].pen, placed[first:])
	}
	return true
}

// drawLines draws the lines of the graph in the rectangle of its grid: the
// layer, moved along from where it was stroked to now by whole pixels of the
// screen, which keep it as sharp as it was stroked, and each line that holds
// drawn on from its newest point, unchanged, to the right edge.
func (g *graph) drawLines(p *qt.QPainter, r chart.Rect) {
	ratio := g.look.ratio
	dx := math.Round(-g.now.Sub(g.curvesAt).Seconds()/g.span.Seconds()*r.W*ratio) / ratio
	p.Translate2(dx, 0)
	p.DrawPixmap9(0, 0, g.layer)
	p.Translate2(-dx, 0)
	for _, c := range g.curves {
		if c.hold && g.now.After(c.newest.T) {
			p.SetPenWithPen(c.pen)
			drawLine(p, c.last.X+dx, c.last.Y, r.X+r.W, c.last.Y)
		}
	}
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
