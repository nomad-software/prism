package ui

import (
	"math"
	"runtime"
	"strconv"
	"time"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/prism/internal/chart"
	"github.com/nomad-software/prism/internal/scene"
	"github.com/nomad-software/prism/internal/stats"
	"github.com/nomad-software/prism/internal/usage"
)

// picture is what a screen draws the processes as: the skyline or the
// territories.
type picture interface {
	// layout places the processes in the screen of the size; read says
	// that they have been read again, rather than the screen resized.
	layout(s *screen, width, height float64, read bool)
	// drawUnder draws what is under the processes and drawOver what is
	// over them, which do not move; they are drawn only when the processes
	// are laid out. drawMoving draws what moves, at the time, every frame.
	drawUnder(p *qt.QPainter, s *screen, width, height float64)
	drawMoving(p *qt.QPainter, s *screen, now time.Time)
	drawOver(p *qt.QPainter, s *screen, width, height float64)
	// moving says whether what drawMoving draws changes with time, so that
	// the screen is drawn again each frame.
	moving(now time.Time) bool
	// outline returns the outline of the process of the index, which is
	// ringed when it is chosen or pointed at, or nil.
	outline(i int) *qt.QPainterPath
	// at returns the index of the process at the point, or -1.
	at(x, y float64) int
}

// screen draws the processes as a picture, on a dark screen of its own in
// whatever theme, in neon: hotter colours for busier processes. What does not
// move is drawn into a pixmap under the processes and one over them when the
// processes are read, or it is resized; each frame draws what moves between
// them, then rings the process chosen and the one pointed at, and draws a card
// of what is known of one of them in its top right corner.
type screen struct {
	*qt.QWidget
	picture picture
	// title is written in its top left corner, above the caption, which
	// says what its shapes and colours mean, in the accent colour that its
	// frame is drawn in.
	title, caption string
	accent         scene.RGB
	procs          []usage.Process
	units          chart.Units
	// chosen is the process chosen, if one is, and hovered the index of the
	// one the pointer is over, or -1.
	chosen  *stats.ProcessKey
	hovered int
	// choose is called with the index of the process clicked, or -1 when
	// none is.
	choose func(i int)
	// under and over are what is under and over the processes as they were
	// last drawn, at look; stale says that they have changed since, and read
	// that they have been read again.
	under, over *qt.QPixmap
	look        screenLook
	stale, read bool
	// now is the time the screen is drawn at, which frames move on, and
	// cost how long it last took to draw, by the clock on the wall.
	now  time.Time
	cost time.Duration
}

// frameShare is the most of a processor's time that drawing a picture
// every frame may take: a picture that takes longer to draw is drawn less
// often.
const frameShare = 4

// frameTime returns how often the screen is drawn while it moves: as often as
// frames can be, unless drawing it takes more than a frameShare of that.
func (s *screen) frameTime() time.Duration {
	return min(max(minFrame, s.cost*frameShare), maxFrame)
}

// screenLook is what a screen's picture is drawn at: its size, the ratio of
// its pixels, and its font.
type screenLook struct {
	width, height int
	ratio         float64
	font          string
}

// The parts of a screen, in pixels: the margin inside its frame, the room
// at its top for its title and at its bottom for the key to its colours,
// and the size of the card.
const (
	screenMargin = 24
	screenTop    = 72
	screenBottom = 44
	cardWidth    = 236
	cardHeight   = 118
)

// newScreen returns a screen of the picture, with its title, caption and
// accent colour.
func newScreen(pic picture, title, caption string, accent scene.RGB) *screen {
	s := &screen{QWidget: qt.NewQWidget2(), picture: pic, title: title, caption: caption, accent: accent, hovered: -1}
	s.SetSizePolicy2(qt.QSizePolicy__Expanding, qt.QSizePolicy__Expanding)
	s.SetMinimumSize2(320, 240)
	s.SetMouseTracking(true)
	s.SetAccessibleName(title)
	s.OnPaintEvent(func(_ func(*qt.QPaintEvent), _ *qt.QPaintEvent) { s.paint() })
	s.OnMouseMoveEvent(func(super func(*qt.QMouseEvent), e *qt.QMouseEvent) {
		super(e)
		pos := e.Position()
		s.hover(s.at(pos.X(), pos.Y()))
	})
	s.OnLeaveEvent(func(super func(*qt.QEvent), e *qt.QEvent) {
		super(e)
		s.hover(-1)
	})
	s.OnMousePressEvent(func(super func(*qt.QMouseEvent), e *qt.QMouseEvent) {
		if e.Button() != qt.LeftButton {
			super(e)
			return
		}
		pos := e.Position()
		if s.choose != nil {
			s.choose(s.at(pos.X(), pos.Y()))
		}
	})
	return s
}

// show has the screen draw the processes, read at the time, in the units,
// with the one of the key chosen, if it is not nil.
func (s *screen) show(now time.Time, procs []usage.Process, u chart.Units, chosen *stats.ProcessKey) {
	s.now, s.procs, s.units, s.chosen = now, procs, u, chosen
	s.hovered = -1
	s.stale, s.read = true, true
	s.Update()
}

// advance moves the screen on to the time, drawing it again if it is shown,
// its picture moves, and it is time it was drawn again.
func (s *screen) advance(now time.Time) {
	if !s.animating() {
		s.now = now
		return
	}
	if now.Sub(s.now) >= s.frameTime()-time.Millisecond {
		s.now = now
		s.Update()
	}
}

// animating says whether the screen is shown and its picture moves, as it is
// then drawn every frame.
func (s *screen) animating() bool {
	return s.IsVisible() && s.picture.moving(s.now)
}

// setChosen rings the process of the key, or none if it is nil.
func (s *screen) setChosen(chosen *stats.ProcessKey) {
	s.chosen = chosen
	s.Update()
}

// at returns the index of the process at the point, laid out as the screen
// is now, or -1.
func (s *screen) at(x, y float64) int {
	s.fresh()
	if i := s.picture.at(x, y); i >= 0 && i < len(s.procs) {
		return i
	}
	return -1
}

// hover rings the process of the index, which the pointer is over, with a
// pointing hand, or none for -1.
func (s *screen) hover(i int) {
	if i == s.hovered {
		return
	}
	s.hovered = i
	if i >= 0 {
		cursor := qt.NewQCursor2(qt.PointingHandCursor)
		s.SetCursor(cursor)
		cursor.Delete()
	} else {
		s.UnsetCursor()
	}
	s.Update()
}

// chosenIndex returns the index of the process chosen, or -1.
func (s *screen) chosenIndex() int {
	if s.chosen == nil {
		return -1
	}
	for i, p := range s.procs {
		if p.Key() == *s.chosen {
			return i
		}
	}
	return -1
}

// fresh lays the processes out again, and draws them into the pixmap, if
// they or the size, ratio or font of the screen have changed since they
// last were.
func (s *screen) fresh() {
	font := s.Font()
	defer runtime.KeepAlive(font)
	want := screenLook{width: s.Width(), height: s.Height(), ratio: s.DevicePixelRatioF(), font: font.Key()}
	if s.under != nil && !s.stale && want == s.look {
		return
	}
	s.look, s.stale = want, false
	w, h := float64(want.width), float64(want.height)
	s.picture.layout(s, w, h, s.read)
	s.read = false
	if s.under != nil {
		s.under.Delete()
		s.over.Delete()
	}
	l := look{width: want.width, height: want.height, ratio: want.ratio}
	s.under, s.over = newLookPixmap(l), newLookPixmap(l)
	p := qt.NewQPainter2(s.under.QPaintDevice)
	p.SetRenderHint(qt.QPainter__Antialiasing)
	s.picture.drawUnder(p, s, w, h)
	p.End()
	p.Delete()
	p = qt.NewQPainter2(s.over.QPaintDevice)
	p.SetRenderHint(qt.QPainter__Antialiasing)
	s.picture.drawOver(p, s, w, h)
	s.drawHeatKey(p, h)
	s.drawFrame(p, w, h)
	p.End()
	p.Delete()
}

// paint draws the picture as it is now, the processes chosen and pointed at
// ringed over it, and the card of the one pointed at, or else of the one
// chosen.
func (s *screen) paint() {
	start := time.Now()
	defer func() { s.cost = time.Since(start) }()
	s.fresh()
	p := qt.NewQPainter2(s.QPaintDevice)
	defer p.Delete()
	defer p.End()
	p.DrawPixmap9(0, 0, s.under)
	p.SetRenderHint(qt.QPainter__Antialiasing)
	s.picture.drawMoving(p, s, s.now)
	p.DrawPixmap9(0, 0, s.over)
	chosen := s.chosenIndex()
	if chosen >= 0 {
		s.ring(p, chosen, scene.RGB{R: 0xff, G: 0xff, B: 0xff}, 2)
	}
	if s.hovered >= 0 && s.hovered != chosen {
		s.ring(p, s.hovered, s.accent, 1.5)
	}
	switch {
	case s.hovered >= 0:
		s.drawCard(p, s.procs[s.hovered], "SCANNING")
	case chosen >= 0:
		s.drawCard(p, s.procs[chosen], "TARGET LOCKED")
	}
}

// ring draws the outline of the process of the index, glowing in the colour.
func (s *screen) ring(p *qt.QPainter, i int, colour scene.RGB, width float64) {
	path := s.picture.outline(i)
	if path == nil {
		return
	}
	glow(p, path, colour, 1, width)
	path.Delete()
}

// fixedFont returns a font of fixed width, at the size of the screen's own
// font times the scale, which the caller deletes. It is the screen's font
// with another family, not the system's fixed font, which miqt returns as a
// copy its finalizer frees off the main thread, while Qt shares fonts.
func (s *screen) fixedFont(scale float64, bold bool) *qt.QFont {
	font := s.Font()
	defer runtime.KeepAlive(font)
	f := qt.NewQFont5(font)
	f.SetFamilies([]string{"monospace", "Menlo"})
	f.SetStyleHint(qt.QFont__TypeWriter)
	if size := font.PointSizeF(); size > 0 {
		f.SetPointSizeF(max(size*scale, 7))
	}
	f.SetBold(bold)
	return f
}

// drawFrame draws the screen's frame, in its accent colour, with brackets in
// its corners, lines across it as an old screen has, and its title and
// caption.
func (s *screen) drawFrame(p *qt.QPainter, w, h float64) {
	// The lines across.
	lines := qt.NewQPainterPath()
	for y := 0.0; y < h; y += 3 {
		lines.AddRect2(0, y, w, 1)
	}
	fillPath(p, lines, scene.RGB{}, 0.07)
	lines.Delete()

	frame := qt.NewQPainterPath()
	frame.AddRoundedRect2(0.5, 0.5, w-1, h-1, 6, 6)
	strokePath(p, frame, s.accent, 0.35, 1)
	frame.Delete()
	const bracket, inset = 18, 6
	corners := qt.NewQPainterPath()
	for _, c := range [][4]float64{{inset, inset, 1, 1}, {w - inset, inset, -1, 1}, {inset, h - inset, 1, -1}, {w - inset, h - inset, -1, -1}} {
		corners.MoveTo2(c[0], c[1]+c[3]*bracket)
		corners.LineTo2(c[0], c[1])
		corners.LineTo2(c[0]+c[2]*bracket, c[1])
	}
	strokePath(p, corners, s.accent, 1, 2)
	corners.Delete()

	title := s.fixedFont(1.4, true)
	defer title.Delete()
	p.SetFont(title)
	drawGlowText(p, screenMargin, screenMargin-4, w, 30, int(qt.AlignLeft|qt.AlignVCenter), s.title, s.accent, 1)
	caption := s.fixedFont(0.8, false)
	defer caption.Delete()
	p.SetFont(caption)
	drawColourText(p, screenMargin, screenMargin+26, w-2*screenMargin-cardWidth, 20, int(qt.AlignLeft|qt.AlignVCenter), s.caption, white, 0.55)
}

// white is the colour of text on a screen.
var white = scene.RGB{R: 0xe6, G: 0xe6, B: 0xe6}

// drawHeatKey draws the key to the colours of heat, from cold to hot, along
// the bottom left of the screen.
func (s *screen) drawHeatKey(p *qt.QPainter, h float64) {
	const width = 160
	x, y := float64(screenMargin), h-screenMargin-18
	font := s.fixedFont(0.75, false)
	defer font.Delete()
	p.SetFont(font)
	drawColourText(p, x, y-2, 40, 12, int(qt.AlignLeft|qt.AlignVCenter), "CPU", white, 0.6)
	gradient := qt.NewQLinearGradient3(x+40, 0, x+40+width, 0)
	for i := range 6 {
		t := float64(i) / 5
		c := qcolour(scene.Heat(t), 1)
		gradient.SetColorAt(t, c)
		c.Delete()
	}
	brush := qt.NewQBrush10(gradient.QGradient)
	bar := qt.NewQRectF4(x+40, y, width, 8)
	p.FillRect(bar, brush)
	bar.Delete()
	brush.Delete()
	gradient.Delete()
	drawColourText(p, x+40, y+10, width, 12, int(qt.AlignLeft|qt.AlignVCenter), "0%", white, 0.5)
	drawColourText(p, x+40, y+10, width, 12, int(qt.AlignRight|qt.AlignVCenter), "100%+", white, 0.5)
}

// drawCard draws a card of what is known of the process in the top right
// corner, under the heading: its name and PID, how busy it is and how many
// processors' worth that is, and its memory.
func (s *screen) drawCard(p *qt.QPainter, proc usage.Process, heading string) {
	w := float64(s.Width())
	x, y := w-screenMargin-cardWidth, float64(screenMargin)
	const notch = 12
	card := qt.NewQPainterPath()
	card.MoveTo2(x+notch, y)
	card.LineTo2(x+cardWidth, y)
	card.LineTo2(x+cardWidth, y+cardHeight-notch)
	card.LineTo2(x+cardWidth-notch, y+cardHeight)
	card.LineTo2(x, y+cardHeight)
	card.LineTo2(x, y+notch)
	card.CloseSubpath()
	fillPath(p, card, scene.RGB{R: 5, G: 8, B: 18}, 0.9)
	glow(p, card, s.accent, 0.9, 1)
	card.Delete()

	small := s.fixedFont(0.8, true)
	defer small.Delete()
	p.SetFont(small)
	drawColourText(p, x+14, y+10, cardWidth-28, 16, int(qt.AlignLeft|qt.AlignVCenter), heading, s.accent, 1)
	drawColourText(p, x+14, y+10, cardWidth-28, 16, int(qt.AlignRight|qt.AlignVCenter), "PID "+strconv.Itoa(proc.PID), white, 0.6)
	name := s.fixedFont(1.15, true)
	defer name.Delete()
	p.SetFont(name)
	drawColourText(p, x+14, y+30, cardWidth-28, 22, int(qt.AlignLeft|qt.AlignVCenter), proc.Name, white, 1)

	cells := processCells(proc, s.units)
	largest := uint64(0)
	for _, q := range s.procs {
		largest = max(largest, q.Memory)
	}
	memory := 0.0
	if largest > 0 {
		memory = float64(proc.Memory) / float64(largest)
	}
	rows := []struct {
		label, value string
		bar          float64
	}{
		{"CPU", cells[colProcessCPU], scene.CPUHeat(proc)},
		{"CORES", cells[colProcessCores], -1},
		{"MEM", cells[colProcessMemory], memory},
	}
	text := s.fixedFont(0.8, false)
	defer text.Delete()
	p.SetFont(text)
	for i, row := range rows {
		ry := y + 60 + float64(i)*18
		drawColourText(p, x+14, ry, 60, 14, int(qt.AlignLeft|qt.AlignVCenter), row.label, white, 0.55)
		drawColourText(p, x+14, ry, cardWidth-28, 14, int(qt.AlignRight|qt.AlignVCenter), row.value, white, 1)
		if row.bar < 0 {
			continue
		}
		const segments = 14
		lit := int(math.Ceil(min(row.bar, 1) * segments))
		for k := range segments {
			alpha := 0.12
			if k < lit {
				alpha = 0.95
			}
			c := qcolour(scene.Heat(float64(k)/segments), alpha)
			segment := qt.NewQRectF4(x+70+float64(k)*7, ry+3, 5, 9)
			p.FillRect4(segment, c)
			segment.Delete()
			c.Delete()
		}
	}
}

// busy returns how busy the process was, or 0 while that is not known.
func busy(p usage.Process) float64 {
	if !p.Measured {
		return 0
	}
	return p.Busy
}

// qcolour returns the colour, at the opacity, which the caller deletes.
func qcolour(c scene.RGB, alpha float64) *qt.QColor {
	return qt.NewQColor11(int(c.R), int(c.G), int(c.B), int(math.Round(min(max(alpha, 0), 1)*255)))
}

// shade returns the colour darkened, or lightened above 1, by the factor.
func shade(c scene.RGB, f float64) scene.RGB {
	part := func(v uint8) uint8 { return uint8(min(math.Round(float64(v)*f), 255)) }
	return scene.RGB{R: part(c.R), G: part(c.G), B: part(c.B)}
}

// fillPath fills the path in the colour, at the opacity.
func fillPath(p *qt.QPainter, path *qt.QPainterPath, colour scene.RGB, alpha float64) {
	c := qcolour(colour, alpha)
	brush := qt.NewQBrush3(c)
	p.FillPath(path, brush)
	brush.Delete()
	c.Delete()
}

// strokePath strokes the path in the colour, at the opacity and width.
func strokePath(p *qt.QPainter, path *qt.QPainterPath, colour scene.RGB, alpha, width float64) {
	c := qcolour(colour, alpha)
	pen := qt.NewQPen3(c)
	pen.SetWidthF(width)
	pen.SetCapStyle(qt.RoundCap)
	pen.SetJoinStyle(qt.RoundJoin)
	p.StrokePath(path, pen)
	pen.Delete()
	c.Delete()
}

// glow strokes the path in the colour, at the opacity and width, over wider
// and fainter strokes of it, which glow around it as neon does.
func glow(p *qt.QPainter, path *qt.QPainterPath, colour scene.RGB, alpha, width float64) {
	strokePath(p, path, colour, alpha*0.08, width*6)
	strokePath(p, path, colour, alpha*0.18, width*3)
	strokePath(p, path, colour, alpha, width)
}

// radialGlow fills a circle of the radius with the colour, at the opacity in
// its middle, fading to nothing at its edge.
func radialGlow(p *qt.QPainter, x, y, radius float64, colour scene.RGB, alpha float64) {
	if radius <= 0 {
		return
	}
	gradient := qt.NewQRadialGradient5(x, y, radius)
	inner, outer := qcolour(colour, alpha), qcolour(colour, 0)
	gradient.SetColorAt(0, inner)
	gradient.SetColorAt(1, outer)
	brush := qt.NewQBrush10(gradient.QGradient)
	circle := qt.NewQPainterPath()
	circle.AddEllipse2(x-radius, y-radius, 2*radius, 2*radius)
	p.FillPath(circle, brush)
	circle.Delete()
	brush.Delete()
	inner.Delete()
	outer.Delete()
	gradient.Delete()
}

// drawColourText draws text in the colour, at the opacity, in a rectangle,
// aligned by the flags.
func drawColourText(p *qt.QPainter, x, y, w, h float64, flags int, text string, colour scene.RGB, alpha float64) {
	c := qcolour(colour, alpha)
	p.SetPen(c)
	drawText(p, x, y, w, h, flags, text)
	c.Delete()
}

// drawGlowText draws text in the colour over a faint copy of it, a pixel
// around, as glowing.
func drawGlowText(p *qt.QPainter, x, y, w, h float64, flags int, text string, colour scene.RGB, alpha float64) {
	for _, d := range [][2]float64{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
		drawColourText(p, x+d[0], y+d[1], w, h, flags, text, colour, alpha*0.18)
	}
	drawColourText(p, x, y, w, h, flags, text, colour, alpha)
}

// drawShadowText draws text in the colour over a dark shadow of it, so that
// it can be read over what is bright.
func drawShadowText(p *qt.QPainter, x, y, w, h float64, flags int, text string, colour scene.RGB, alpha float64) {
	for _, d := range [][2]float64{{-1, 0}, {1, 0}, {0, -1}, {0, 1}, {1, 1}} {
		drawColourText(p, x+d[0], y+d[1], w, h, flags, text, scene.RGB{}, 0.8)
	}
	drawColourText(p, x, y, w, h, flags, text, colour, alpha)
}

// fillGradient fills the rectangle with a gradient from the colour at its
// top to that at its bottom.
func fillGradient(p *qt.QPainter, x, y, w, h float64, top, bottom scene.RGB, topAlpha, bottomAlpha float64) {
	gradient := qt.NewQLinearGradient3(0, y, 0, y+h)
	a, b := qcolour(top, topAlpha), qcolour(bottom, bottomAlpha)
	gradient.SetColorAt(0, a)
	gradient.SetColorAt(1, b)
	brush := qt.NewQBrush10(gradient.QGradient)
	r := qt.NewQRectF4(x, y, w, h)
	p.FillRect(r, brush)
	r.Delete()
	brush.Delete()
	a.Delete()
	b.Delete()
	gradient.Delete()
}

// polygonPath returns the closed path through the points, which the caller
// deletes.
func polygonPath(points []chart.XY) *qt.QPainterPath {
	path := qt.NewQPainterPath()
	for i, pt := range points {
		if i == 0 {
			path.MoveTo2(pt.X, pt.Y)
		} else {
			path.LineTo2(pt.X, pt.Y)
		}
	}
	path.CloseSubpath()
	return path
}

// hash returns a number from 0 to 1 that is always the same for the same
// numbers, as the windows of a tower are lit by.
func hash(numbers ...int) float64 {
	h := uint64(14695981039346656037)
	for _, n := range numbers {
		h ^= uint64(n)
		h *= 1099511628211
		h ^= h >> 29
	}
	return float64(h%10007) / 10007
}
