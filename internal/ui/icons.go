package ui

import (
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
