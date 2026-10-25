package ui

import (
	"fmt"
	"runtime"

	qt "github.com/mappu/miqt/qt6"
)

// The tabs of the window, in order.
const (
	tabResources = iota
	tabProcesses
	tabCount
)

// tabPadX and tabPadY are the room added to each tab beyond what the style
// gives it, across and down.
const (
	tabPadX = 20
	tabPadY = 10
)

// tabIconSize is the size the icons of the tabs are drawn at, in pixels at a
// ratio of one, and tabIconPen the width of their lines.
const (
	tabIconSize = 16
	tabIconPen  = 1.5
)

// makeTabs puts the pages in tabs, each with an icon, and a shortcut to each,
// Ctrl+1 and Ctrl+2 (⌘1 and ⌘2 on macOS). The icons are drawn in the colour of
// text, and drawn again whenever the palette or style changes, as with the
// theme.
func (w *Window) makeTabs(resources, processes *qt.QWidget) {
	w.tabs = qt.NewQTabWidget2()
	// The tabs are as the style draws them, with more room in them.
	bar := qt.NewQTabBar2()
	sizes := make([]*qt.QSize, tabCount)
	bar.OnTabSizeHint(func(super func(int) *qt.QSize, index int) *qt.QSize {
		size := super(index)
		if index < 0 || index >= len(sizes) {
			return size
		}
		// What is returned is copied once this returns, so it is kept here,
		// where the collector cannot free it before then.
		if sizes[index] == nil {
			sizes[index] = qt.NewQSize()
		}
		sizes[index].SetWidth(size.Width() + tabPadX)
		sizes[index].SetHeight(size.Height() + tabPadY)
		return sizes[index]
	})
	w.tabs.SetTabBar(bar)
	w.tabs.SetDocumentMode(true)
	w.tabs.AddTab(resources, "Resources")
	w.tabs.AddTab(processes, "Processes")
	w.drawTabIcons()
	for i := range tabCount {
		key := qt.NewQKeySequence2(fmt.Sprintf("Ctrl+%d", i+1))
		shortcut := qt.NewQShortcut2(key, w.QObject)
		key.Delete()
		shortcut.OnActivated(func() { w.tabs.SetCurrentIndex(i) })
		w.tabKeys = append(w.tabKeys, shortcut)
	}
	w.OnChangeEvent(func(super func(*qt.QEvent), event *qt.QEvent) {
		super(event)
		switch event.Type() {
		case qt.QEvent__PaletteChange, qt.QEvent__ApplicationPaletteChange, qt.QEvent__StyleChange:
			w.drawTabIcons()
		}
	})
}

// drawTabIcons gives each tab its icon, in the colour of text: a line graph in
// a frame for the resources, and a list for the processes.
func (w *Window) drawTabIcons() {
	// The application's palette is the theme's at once; when the window
	// hears of a change, the tabs can still have the palette before it.
	palette := qt.QGuiApplication_Palette()
	defer runtime.KeepAlive(palette)
	text := palette.ColorWithCr(qt.QPalette__WindowText)
	for i, draw := range [tabCount]func(*qt.QPainter){drawResourcesIcon, drawProcessesIcon} {
		icon := tabIcon(text, w.DevicePixelRatioF(), draw)
		w.tabs.SetTabIcon(i, icon)
		icon.Delete()
	}
}

// tabIcon returns an icon drawn by draw, on a clear square of tabIconSize, in
// the colour, at the ratio of pixels the window has.
func tabIcon(colour *qt.QColor, ratio float64, draw func(*qt.QPainter)) *qt.QIcon {
	ratio = max(ratio, 1)
	side := int(tabIconSize * ratio)
	pixmap := qt.NewQPixmap2(side, side)
	defer pixmap.Delete()
	pixmap.SetDevicePixelRatio(ratio)
	clear := qt.NewQColor2(qt.Transparent)
	pixmap.FillWithFillColor(clear)
	clear.Delete()

	p := qt.NewQPainter2(pixmap.QPaintDevice)
	p.SetRenderHint(qt.QPainter__Antialiasing)
	pen := qt.NewQPen3(colour)
	pen.SetWidthF(tabIconPen)
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
	path := qt.NewQPainterPath()
	defer path.Delete()
	for i, pt := range [][2]float64{{4, 10.5}, {6.5, 7}, {9, 9}, {12, 5}} {
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

// drawProcessesIcon draws a list of three rows, each a dot and a line.
func drawProcessesIcon(p *qt.QPainter) {
	for _, y := range []float64{3.5, 8, 12.5} {
		dot := qt.NewQPointF3(2.5, y)
		p.DrawEllipse3(dot, 0.6, 0.6)
		dot.Delete()
		drawLine(p, 6, y, 14, y)
	}
}
