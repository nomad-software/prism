package ui

import (
	"runtime"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/prism/internal/chart"
)

// entry is an entry of a legend: the colour of its line, its name, and what
// it says of it. setColour, where it is set, changes the colour in the
// settings, which clicking the swatch does.
type entry struct {
	colour, label, value string
	setColour            func(colour string)
}

// legend names the lines of a graph and says what each shows now. Its
// entries are laid out in columns of equal width, as many as fit, row by
// row, and it is as tall as its rows. Each entry has its name and its value
// side by side, with the values in a column of their own. The values are
// at the right of their column, as the use of the processors is; or, in a
// faint legend, fainter than the names and at the left of their column, as
// the use of memory and the rates of the network adapters are.
type legend struct {
	*qt.QWidget
	entries []entry
	faint   bool
	// labelWidth and valueWidth are the widest names and values there have
	// been, which the columns are kept as wide as, so that they do not move
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
	l := &legend{QWidget: qt.NewQWidget2(), faint: faint, hovered: -1}
	l.SetSizePolicy2(qt.QSizePolicy__Expanding, qt.QSizePolicy__Fixed)
	l.SetFixedHeight(0)
	l.OnPaintEvent(func(_ func(*qt.QPaintEvent), _ *qt.QPaintEvent) { l.paint() })
	// A swatch is a shortcut to the setting of its colour: the pointer is
	// a hand over it, and clicking it chooses another.
	l.SetMouseTracking(true)
	l.OnMouseMoveEvent(func(super func(*qt.QMouseEvent), e *qt.QMouseEvent) {
		super(e)
		// The position is a copy the garbage collector frees, as is any
		// value miqt returns by value: deleting it as well frees it twice.
		pos := e.Position()
		l.hover(l.settableAt(pos.X(), pos.Y()))
	})
	l.OnLeaveEvent(func(super func(*qt.QEvent), e *qt.QEvent) {
		super(e)
		l.hover(-1)
	})
	l.OnMousePressEvent(func(super func(*qt.QMouseEvent), e *qt.QMouseEvent) {
		pos := e.Position()
		if e.Button() != qt.LeftButton || !l.click(pos.X(), pos.Y()) {
			super(e)
		}
	})
	l.OnResizeEvent(func(super func(*qt.QResizeEvent), e *qt.QResizeEvent) {
		super(e)
		l.fit()
	})
	return l
}

// show has the legend show the entries.
func (l *legend) show(entries []entry) {
	l.entries = entries
	fm := l.FontMetrics()
	defer runtime.KeepAlive(fm)
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

// entryHeight is how tall each entry is: as tall as its swatch, or as its
// line of text.
func (l *legend) entryHeight(lineHeight int) int {
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
	defer runtime.KeepAlive(fm)
	return g.Rows*l.entryHeight(fm.Height()) + (g.Rows-1)*rowGap
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
	y = float64(row*(l.entryHeight(lineHeight)+rowGap)) + float64(max(lineHeight-swatchHeight, 0))/2
	return x, y
}

// swatchAt returns the index of the entry whose swatch is at the place, or
// -1 if there is none.
func (l *legend) swatchAt(x, y float64) int {
	fm := l.FontMetrics()
	defer runtime.KeepAlive(fm)
	g := l.grid()
	for i := range l.entries {
		sx, sy := l.swatchPlace(i, g, fm.Height())
		if x >= sx && x < sx+swatchWidth && y >= sy && y < sy+swatchHeight {
			return i
		}
	}
	return -1
}

// settableAt returns the index of the entry whose swatch is at the place,
// and whose colour can be set, or -1 if there is none.
func (l *legend) settableAt(x, y float64) int {
	if i := l.swatchAt(x, y); i >= 0 && l.entries[i].setColour != nil {
		return i
	}
	return -1
}

// click chooses the colour of the entry whose swatch is at the place, and
// says whether there was one.
func (l *legend) click(x, y float64) bool {
	i := l.settableAt(x, y)
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
		cursor := qt.NewQCursor2(qt.PointingHandCursor)
		defer cursor.Delete()
		l.SetCursor(cursor)
	} else {
		l.UnsetCursor()
	}
	l.Update()
}

// paint draws the legend.
func (l *legend) paint() {
	p := qt.NewQPainter2(l.QPaintDevice)
	defer p.Delete()
	defer p.End()
	p.SetRenderHint(qt.QPainter__Antialiasing)

	fm := l.FontMetrics()
	defer runtime.KeepAlive(fm)
	lineHeight := fm.Height()
	height := l.entryHeight(lineHeight)
	width := l.entryWidth()
	g := l.grid()

	palette := l.Palette()
	defer runtime.KeepAlive(palette)
	text := palette.ColorWithCr(qt.QPalette__WindowText)
	faint := qt.NewQColor10(text)
	defer faint.Delete()
	faint.SetAlphaF(faintOpacity)

	for i, e := range l.entries {
		column, row := g.Cell(i)
		x := float64(column * (width + columnGap))
		y := float64(row * (height + rowGap))

		drawSwatch(p, e.colour, x, y+float64(max(lineHeight-swatchHeight, 0))/2)
		if i == l.hovered {
			drawRing(p, text, x, y+float64(max(lineHeight-swatchHeight, 0))/2)
		}

		x += swatchWidth + legendGap
		p.SetPen(text)
		drawText(p, x, y, float64(l.labelWidth), float64(lineHeight), int(qt.AlignLeft|qt.AlignVCenter), e.label)
		align := qt.AlignRight
		if l.faint {
			p.SetPen(faint)
			align = qt.AlignLeft
		}
		drawText(p, x+float64(l.labelWidth+legendGap), y, float64(l.valueWidth), float64(lineHeight), int(align|qt.AlignVCenter), e.value)
	}
}

// ringWidth is how thick the ring around a swatch the pointer is over is.
const ringWidth = 1.5

// drawRing draws a ring in the colour just inside the edge of the swatch
// with its top left corner at the place, where the edge of the legend does
// not cut it off.
func drawRing(p *qt.QPainter, colour *qt.QColor, x, y float64) {
	pen := qt.NewQPen3(colour)
	defer pen.Delete()
	pen.SetWidthF(ringWidth)
	p.SetPenWithPen(pen)
	p.SetBrushWithStyle(qt.NoBrush)
	inset := ringWidth / 2
	r := qt.NewQRectF4(x+inset, y+inset, swatchWidth-2*inset, swatchHeight-2*inset)
	defer r.Delete()
	p.DrawRoundedRect(r, 3, 3)
}

// drawSwatch draws the swatch of a colour, as the settings write it, with
// its top left corner at the place.
func drawSwatch(p *qt.QPainter, colour string, x, y float64) {
	c := qt.NewQColor6(colour)
	defer c.Delete()
	brush := qt.NewQBrush3(c)
	defer brush.Delete()
	r := qt.NewQRectF4(x, y, swatchWidth, swatchHeight)
	defer r.Delete()
	p.SetPenWithStyle(qt.NoPen)
	p.SetBrush(brush)
	p.DrawRoundedRect(r, 3, 3)
}
