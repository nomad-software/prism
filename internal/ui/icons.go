package ui

import (
	"math"

	qt "github.com/mappu/miqt/qt6"
)

// iconSize is the size the icons of the tabs and of the buttons that choose
// how the processes are shown are drawn at, in pixels at a ratio of one, and
// iconPen the width of their lines.
const (
	iconSize = 16
	iconPen  = 1.5
)

// paintedIcon returns an icon drawn by draw, on a clear square of iconSize,
// in the colour, at the ratio of pixels.
func paintedIcon(colour *qt.QColor, ratio float64, draw func(*qt.QPainter)) *qt.QIcon {
	ratio = max(ratio, 1)
	side := int(iconSize * ratio)
	pixmap := qt.NewQPixmap2(side, side)
	defer pixmap.Delete()
	pixmap.SetDevicePixelRatio(ratio)
	clearPixmap(pixmap)

	p := qt.NewQPainter2(pixmap.QPaintDevice)
	p.SetRenderHint(qt.QPainter__Antialiasing)
	pen := qt.NewQPen3(colour)
	pen.SetWidthF(iconPen)
	pen.SetCapStyle(qt.RoundCap)
	pen.SetJoinStyle(qt.RoundJoin)
	p.SetPenWithPen(pen)
	pen.Delete()
	p.SetBrushWithStyle(qt.NoBrush)
	draw(p)
	p.End()
	p.Delete()
	// The icon keeps a copy of the picture.
	return qt.NewQIcon2(pixmap)
}

// drawResourcesIcon draws a line graph in a rounded frame.
func drawResourcesIcon(p *qt.QPainter) {
	frame := qt.NewQRectF4(1.5, 2.5, 13, 11)
	p.DrawRoundedRect(frame, 2, 2)
	frame.Delete()
	drawPolyline(p, [][2]float64{{4, 10.5}, {6.5, 7}, {9, 9}, {12, 5}})
}

// drawProcessesIcon draws a list of three rows, each a node and its text.
func drawProcessesIcon(p *qt.QPainter) {
	for _, y := range []float64{2.5, 7.5, 12.5} {
		drawRow(p, 1, y)
	}
}

// drawTreeIcon draws a tree view: a row at the top, and a trunk from its node
// that branches to two rows indented below it.
func drawTreeIcon(p *qt.QPainter) {
	drawRow(p, 1, 2.5)
	drawRow(p, 6, 7.5)
	drawRow(p, 6, 12.5)
	strokeSharp(p, 1, [2]float64{2.5, 4}, [2]float64{2.5, 13})
	for _, y := range []float64{7.5, 12.5} {
		strokeSharp(p, 1, [2]float64{3, y}, [2]float64{6, y})
	}
}

// drawRow draws a row of the list and tree icons from x, across the middle of
// the pixels at y: a node 3 pixels square and, after a gap, a line a pixel
// wide for its text, to the right of the icon less a pixel.
func drawRow(p *qt.QPainter, x, y float64) {
	strokeSharp(p, 3, [2]float64{x, y}, [2]float64{x + 3, y})
	strokeSharp(p, 1, [2]float64{x + 5, y}, [2]float64{iconSize - 1, y})
}

// strokeSharp draws lines through the points with the pen at the width and
// with square ends that stop at them, so that lines along whole pixels are
// sharp.
func strokeSharp(p *qt.QPainter, width float64, points ...[2]float64) {
	pen := p.Pen()
	pen.SetWidthF(width)
	pen.SetCapStyle(qt.FlatCap)
	pen.SetJoinStyle(qt.MiterJoin)
	p.SetPenWithPen(pen)
	drawPolyline(p, points)
}

// drawPolyline draws lines through the points, in turn, joined as the pen
// joins them.
func drawPolyline(p *qt.QPainter, points [][2]float64) {
	path := qt.NewQPainterPath()
	defer path.Delete()
	for i, pt := range points {
		point := qt.NewQPointF3(pt[0], pt[1])
		if i == 0 {
			path.MoveTo(point)
		} else {
			path.LineTo(point)
		}
		point.Delete()
	}
	p.DrawPath(path)
}

// drawSkylineIcon draws a skyline: three towers of different heights,
// outlined, standing on a line.
func drawSkylineIcon(p *qt.QPainter) {
	for _, tower := range [][3]float64{{2.5, 5.5, 8.5}, {6.5, 9.5, 2.5}, {10.5, 13.5, 6.5}} {
		left, right, top := tower[0], tower[1], tower[2]
		strokeSharp(p, 1, [2]float64{left, 14}, [2]float64{left, top}, [2]float64{right, top}, [2]float64{right, 14})
	}
	strokeSharp(p, 1, [2]float64{1, 14.5}, [2]float64{iconSize - 1, 14.5})
}

// drawTerritoriesIcon draws a honeycomb of three hexagons, the top one
// filled, as a territory.
func drawTerritoriesIcon(p *qt.QPainter) {
	const radius = 3.5
	pen := p.Pen()
	pen.SetWidthF(1.2)
	p.SetPenWithPen(pen)
	across := radius * 1.7320508
	for i, centre := range [][2]float64{{8, 4.5}, {8 - across/2, 9.75}, {8 + across/2, 9.75}} {
		path := qt.NewQPainterPath()
		for corner := range 6 {
			x := centre[0] + radius*cosDegrees(30+60*corner)
			y := centre[1] + radius*sinDegrees(30+60*corner)
			if corner == 0 {
				path.MoveTo2(x, y)
			} else {
				path.LineTo2(x, y)
			}
		}
		path.CloseSubpath()
		p.DrawPath(path)
		path.Delete()
		if i == 0 {
			// The top one is filled by a wide stroke of a hexagon inside it,
			// in the colour of the pen.
			pen.SetWidthF(radius)
			p.SetPenWithPen(pen)
			inner := qt.NewQPainterPath()
			for corner := range 6 {
				x := centre[0] + radius/2*cosDegrees(30+60*corner)
				y := centre[1] + radius/2*sinDegrees(30+60*corner)
				if corner == 0 {
					inner.MoveTo2(x, y)
				} else {
					inner.LineTo2(x, y)
				}
			}
			inner.CloseSubpath()
			p.DrawPath(inner)
			inner.Delete()
			pen.SetWidthF(1.2)
			p.SetPenWithPen(pen)
		}
	}
}

// cosDegrees and sinDegrees are the cosine and sine of the angle in degrees.
func cosDegrees(degrees int) float64 { return math.Cos(float64(degrees) * math.Pi / 180) }
func sinDegrees(degrees int) float64 { return math.Sin(float64(degrees) * math.Pi / 180) }
