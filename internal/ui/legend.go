package ui

import (
	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"

	"github.com/nomad-software/prism/internal/chart"
)

// entry is an entry of a legend: the colour of its line, its name, and what
// it says of it. setColour changes the colour in the settings, which clicking
// the swatch does.
type entry struct {
	colour, label, value string
	setColour            func(colour string)
}

// legend names the lines of a graph and says what each shows now. Its
// entries are laid out in columns of equal width, as many as fit, row by
// row, and it is as tall as its rows. Each entry has its name and its value
// side by side. The values are in a column of their own, at its right, as
// the use of the processors is; or, in a faint legend, fainter than the
// names and straight after each name, as the use of memory and the rates of
// the network adapters are.
type legend struct {
	qtwidgets.QWidgetBase
	entries []entry
	faint   bool
	// labelWidth is the widest name shown, which the names' column is as
	// wide as. valueWidth is the widest value there has been, which the
	// values' column is kept as wide as, so that the columns do not move
	// about as the values change.
	labelWidth, valueWidth int
	// hovered is the index of the entry whose swatch the pointer is over,
	// which is ringed, or -1.
	hovered int
}

// The sizes of the parts of a legend, in pixels.
const (
	swatchWidth  = 28
	swatchHeight = 16
	legendGap    = 8
	columnGap    = 24
	rowGap       = 6
)

// faintOpacity is how opaque the values of a faint legend are.
const faintOpacity = 0.4

// newLegend returns an empty legend, whose values are faint if it is.
func newLegend(faint bool) *legend {
	l := &legend{faint: faint, hovered: -1}
	qtwidgets.NewQWidgetWith(l, nil)
	l.SetSizePolicy(qtwidgets.NewQSizePolicyWithPolicy(qtwidgets.QSizePolicyExpanding, qtwidgets.QSizePolicyFixed))
	l.SetFixedHeight(0)
	// A swatch is a shortcut to the setting of its colour: the pointer is
	// a hand over it, and clicking it chooses another.
	l.SetMouseTracking(true)
	return l
}

// PaintEvent draws the legend.
func (l *legend) PaintEvent(*qtgui.QPaintEvent) {
	l.paint()
}

// MouseMoveEvent rings the swatch the pointer is over.
func (l *legend) MouseMoveEvent(e *qtgui.QMouseEvent) {
	l.Super().MouseMoveEvent(e)
	pos := e.Position()
	l.hover(l.swatchAt(pos.X, pos.Y))
}

// LeaveEvent takes the ring away as the pointer leaves.
func (l *legend) LeaveEvent(e *qtcore.QEvent) {
	l.Super().LeaveEvent(e)
	l.hover(-1)
}

// MousePressEvent chooses the colour of the swatch clicked.
func (l *legend) MousePressEvent(e *qtgui.QMouseEvent) {
	pos := e.Position()
	if e.Button() != qtcore.LeftButton || !l.click(pos.X, pos.Y) {
		l.Super().MousePressEvent(e)
	}
}

// ResizeEvent makes the legend as high as its entries need at its width.
func (l *legend) ResizeEvent(e *qtgui.QResizeEvent) {
	l.Super().ResizeEvent(e)
	l.fit()
}

// show has the legend show the entries.
func (l *legend) show(entries []entry) {
	l.entries = entries
	fm := l.FontMetrics()
	defer fm.Delete()
	// The names change only when an adapter is hidden, shown or renamed, so
	// their column fits those shown now, leaving no gap after the longest.
	l.labelWidth = 0
	for _, e := range entries {
		l.labelWidth = max(l.labelWidth, fm.HorizontalAdvance(e.label))
		l.valueWidth = max(l.valueWidth, fm.HorizontalAdvance(e.value))
	}
	l.fit()
	l.Update()
}

// entryWidth is how wide each entry is.
func (l *legend) entryWidth() int {
	return swatchWidth + legendGap + l.labelWidth + legendGap + l.valueWidth
}

// entryHeight is how tall each entry of a legend is: as tall as its swatch, or
// as its line of text.
func entryHeight(lineHeight int) int {
	return max(swatchHeight, lineHeight)
}

// grid returns how the entries are laid out across the width of the legend.
func (l *legend) grid() chart.Grid {
	return chart.Layout(len(l.entries), float64(l.entryWidth()), columnGap, float64(l.Width()))
}

// height returns how tall the legend is at its width.
func (l *legend) height() int {
	g := l.grid()
	if g.Rows == 0 {
		return 0
	}
	fm := l.FontMetrics()
	defer fm.Delete()
	return g.Rows*entryHeight(fm.Height()) + (g.Rows-1)*rowGap
}

// fit makes the legend as tall as its rows are at its width.
func (l *legend) fit() {
	if h := l.height(); h != l.Height() || l.MinimumHeight() != h {
		l.SetFixedHeight(h)
	}
}

// swatchPlace returns where the top left corner of the swatch of the entry
// is, of the entries laid out in the grid.
func (l *legend) swatchPlace(i int, g chart.Grid, lineHeight int) (x, y float64) {
	column, row := g.Cell(i)
	x = float64(column * (l.entryWidth() + columnGap))
	y = float64(row*(entryHeight(lineHeight)+rowGap)) + float64(max(lineHeight-swatchHeight, 0))/2
	return x, y
}

// swatchAt returns the index of the entry whose swatch is at the place, or
// -1 if there is none.
func (l *legend) swatchAt(x, y float64) int {
	fm := l.FontMetrics()
	defer fm.Delete()
	g := l.grid()
	for i := range l.entries {
		sx, sy := l.swatchPlace(i, g, fm.Height())
		if x >= sx && x < sx+swatchWidth && y >= sy && y < sy+swatchHeight {
			return i
		}
	}
	return -1
}

// click chooses the colour of the entry whose swatch is at the place, and
// says whether there was one.
func (l *legend) click(x, y float64) bool {
	i := l.swatchAt(x, y)
	if i < 0 {
		return false
	}
	// The entries can change while the colour is chosen, as the
	// statistics are read.
	e := l.entries[i]
	l.hover(-1)
	if colour, ok := pickColour(l.QWidget, e.label, e.colour); ok {
		e.setColour(colour)
	}
	return true
}

// hover rings the swatch of the entry, and makes the pointer a hand over it,
// or neither for -1.
func (l *legend) hover(i int) {
	if i == l.hovered {
		return
	}
	l.hovered = i
	if i >= 0 {
		cursor := qtgui.NewQCursorWithCursorShape(qtcore.PointingHandCursor)
		defer cursor.Delete()
		l.SetCursor(cursor)
	} else {
		l.UnsetCursor()
	}
	l.Update()
}

// paint draws the legend.
func (l *legend) paint() {
	qtgui.PaintOn(l, l.paintWith)
}

// paintWith draws the legend with the painter, as paint has it.
func (l *legend) paintWith(p *qtgui.QPainter) {
	p.SetRenderHint(qtgui.QPainterAntialiasing)

	fm := l.FontMetrics()
	defer fm.Delete()
	lineHeight := fm.Height()
	height := entryHeight(lineHeight)
	g := l.grid()

	palette := l.Palette()
	defer palette.Delete()
	text := palette.Color(qtgui.QPaletteWindowText)
	faint := withAlpha(text, faintOpacity)

	for i, e := range l.entries {
		// The swatch is where clicks find it.
		sx, sy := l.swatchPlace(i, g, lineHeight)
		drawSwatch(p, e.colour, sx, sy)
		if i == l.hovered {
			drawRing(p, text, sx, sy)
		}

		_, row := g.Cell(i)
		x := sx + swatchWidth + legendGap
		y := float64(row * (height + rowGap))
		p.SetPenColor(text)
		drawText(p, x, y, float64(l.labelWidth), float64(lineHeight), int(qtcore.AlignLeft|qtcore.AlignVCenter), e.label)
		// The values of the processors are at the right of a column of
		// their own, so that they line up; a faint value follows straight
		// on from its own name.
		vx, align := x+float64(l.labelWidth+legendGap), qtcore.AlignRight
		if l.faint {
			p.SetPenColor(faint)
			vx, align = x+float64(fm.HorizontalAdvance(e.label)+legendGap), qtcore.AlignLeft
		}
		drawText(p, vx, y, float64(l.valueWidth), float64(lineHeight), int(align|qtcore.AlignVCenter), e.value)
	}
}

// ringWidth is how thick the ring around a swatch the pointer is over is.
const ringWidth = 1.5

// drawRing draws a ring in the colour just inside the edge of the swatch
// with its top left corner at the place, where the edge of the legend does
// not cut it off.
func drawRing(p *qtgui.QPainter, colour qtgui.QColor, x, y float64) {
	pen := qtgui.NewQPenWithColor(colour)
	defer pen.Delete()
	pen.SetWidthF(ringWidth)
	p.SetPen(pen)
	p.SetBrushStyle(qtcore.NoBrush)
	inset := ringWidth / 2
	p.DrawRoundedRectF(rectF(x+inset, y+inset, swatchWidth-2*inset, swatchHeight-2*inset), 3, 3)
}

// drawSwatch draws the swatch of a colour, as the settings write it, with
// its top left corner at the place.
func drawSwatch(p *qtgui.QPainter, colour string, x, y float64) {
	p.SetPenStyle(qtcore.NoPen)
	p.SetBrushColor(parseColour(colour))
	p.DrawRoundedRectF(rectF(x, y, swatchWidth, swatchHeight), 3, 3)
}
