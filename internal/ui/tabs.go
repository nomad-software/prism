package ui

import (
	"fmt"

	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"
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

// tabWidget is the tab widget of the window, which is made in Go to be given
// a tab bar of its own, as only a subclass can.
type tabWidget struct {
	qtwidgets.QTabWidgetBase
}

// tabBar is a tab bar whose tabs are as the style draws them, with more room
// in them.
type tabBar struct {
	qtwidgets.QTabBarBase
}

// TabSizeHint gives each tab the room the style gives it, and tabPadX and
// tabPadY more.
func (b *tabBar) TabSizeHint(index int) qtcore.QSize {
	size := b.Super().TabSizeHint(index)
	return qtcore.QSize{Width: size.Width + tabPadX, Height: size.Height + tabPadY}
}

// makeTabs puts the pages in tabs, each with an icon, and a shortcut to each,
// Ctrl+1 and Ctrl+2. The icons are drawn in the colour of text, and drawn
// again whenever the palette or style changes, as with the theme.
func (w *Window) makeTabs(resources, processes *qtwidgets.QWidget) {
	w.tabs = &tabWidget{}
	qtwidgets.NewQTabWidgetWith(w.tabs, w)
	bar := &tabBar{}
	qtwidgets.NewQTabBarWith(bar, nil)
	w.tabs.SetTabBar(bar)
	w.tabs.SetDocumentMode(true)
	w.tabs.AddTab(resources, "Resources")
	w.tabs.AddTab(processes, "Processes")
	// The search has the focus whenever the processes are shown, so that
	// typing narrows them at once.
	w.tabs.OnCurrentChanged(func(index int) {
		if index == tabProcesses {
			w.processes.search.SetFocus()
		}
	})
	w.drawIcons()
	for i := range tabCount {
		key := qtgui.NewQKeySequenceWithKey(fmt.Sprintf("Ctrl+%d", i+1))
		shortcut := qtgui.NewQShortcutWithKeySequence(key, w)
		shortcut.OnActivated(func() { w.tabs.SetCurrentIndex(i) })
		w.tabKeys = append(w.tabKeys, shortcut)
	}
}

// ChangeEvent draws the icons again when the palette or the style changes,
// once the tabs have been made: the theme is applied before they are.
func (w *Window) ChangeEvent(event *qtcore.QEvent) {
	w.Super().ChangeEvent(event)
	if w.tabs == nil {
		return
	}
	switch event.Type() {
	case qtcore.QEventPaletteChange, qtcore.QEventApplicationPaletteChange, qtcore.QEventStyleChange:
		w.drawIcons()
	}
}

// drawIcons gives each tab its icon, in the colour of text: a line graph in
// a frame for the resources, and a list for the processes. The buttons that
// show the processes as a list or a tree have theirs drawn in it too.
func (w *Window) drawIcons() {
	// The application's palette is the theme's at once; when the window
	// hears of a change, the tabs can still have the palette before it.
	text := qtgui.QGuiApplicationPalette().Color(qtgui.QPaletteWindowText)
	ratio := w.AsQPaintDevice().DevicePixelRatioF()
	for i, draw := range [tabCount]func(*qtgui.QPainter){drawResourcesIcon, drawProcessesIcon} {
		w.tabs.SetTabIcon(i, paintedIcon(text, ratio, draw))
	}
	w.processes.view.drawIcons(text, ratio)
}
