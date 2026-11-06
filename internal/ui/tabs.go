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

// makeTabs puts the pages in tabs, each with an icon, and a shortcut to each,
// Ctrl+1 and Ctrl+2 (⌘1 and ⌘2 on macOS). The icons are drawn in the colour of
// text, and drawn again whenever the palette or style changes, as with the
// theme.
func (w *Window) makeTabs(resources, processes *qt.QWidget) {
	w.tabs = qt.NewQTabWidget2()
	// The tabs are as the style draws them, with more room in them.
	bar := qt.NewQTabBar2()
	// What is returned is copied once it is returned, so the size of each
	// tab is kept here, where the collector cannot free it before then.
	sizes := map[int]*qt.QSize{}
	bar.OnTabSizeHint(func(super func(int) *qt.QSize, index int) *qt.QSize {
		size := super(index)
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
	// The search has the focus whenever the processes are shown, so that
	// typing narrows them at once.
	w.tabs.OnCurrentChanged(func(index int) {
		if index == tabProcesses {
			w.processes.search.SetFocus()
		}
	})
	w.drawIcons()
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
			w.drawIcons()
		}
	})
}

// drawIcons gives each tab its icon, in the colour of text: a line graph in
// a frame for the resources, and a list for the processes. The buttons that
// show the processes as a list or a tree have theirs drawn in it too.
func (w *Window) drawIcons() {
	// The application's palette is the theme's at once; when the window
	// hears of a change, the tabs can still have the palette before it.
	palette := qt.QGuiApplication_Palette()
	defer runtime.KeepAlive(palette)
	text := palette.ColorWithCr(qt.QPalette__WindowText)
	ratio := w.DevicePixelRatioF()
	for i, draw := range [tabCount]func(*qt.QPainter){drawResourcesIcon, drawProcessesIcon} {
		icon := paintedIcon(text, ratio, draw)
		w.tabs.SetTabIcon(i, icon)
		icon.Delete()
	}
	w.processes.view.drawIcons(text, ratio)
}
