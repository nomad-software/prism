package ui

import (
	"runtime"

	qt "github.com/mappu/miqt/qt6"
)

// menuButtonSize is the width and height of the button of the main menu.
const menuButtonSize = 40

// newMenuButton returns the button of the main menu: three lines across, as
// GNOME's are, drawn in the colour of the theme's text over what the style
// draws of the button, so that they follow the theme as it changes.
func newMenuButton() *qt.QToolButton {
	b := qt.NewQToolButton2()
	b.SetAutoRaise(true)
	b.SetFixedSize2(menuButtonSize, menuButtonSize)
	b.SetToolTip("Main menu")
	b.SetAccessibleName("Main menu")
	b.OnPaintEvent(func(super func(*qt.QPaintEvent), event *qt.QPaintEvent) {
		super(event)
		p := qt.NewQPainter2(b.QPaintDevice)
		defer p.Delete()
		p.SetRenderHint(qt.QPainter__Antialiasing)
		palette := b.Palette()
		defer runtime.KeepAlive(palette)
		brush := qt.NewQBrush3(palette.ColorWithCr(qt.QPalette__ButtonText))
		defer brush.Delete()
		pen := qt.NewQPen7(brush, 2.5, qt.SolidLine, qt.RoundCap)
		defer pen.Delete()
		p.SetPenWithPen(pen)
		// The lines are 20 wide and 6 apart, in the middle of the button.
		cx, cy := float64(b.Width())/2, float64(b.Height())/2
		for _, dy := range []float64{-6, 0, 6} {
			drawLine(p, cx-10, cy+dy, cx+10, cy+dy)
		}
	})
	return b
}
