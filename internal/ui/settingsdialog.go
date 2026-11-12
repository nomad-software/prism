package ui

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/nomad-software/qute/qtcore"
	"github.com/nomad-software/qute/qtgui"
	"github.com/nomad-software/qute/qtwidgets"

	"github.com/nomad-software/prism/internal/settings"
)

// settingsDialog shows the settings: how often each statistic and the
// processes are read, how the processors are shown, the top of the memory
// graph, the units sizes and rates are written in, which network adapters are
// shown and by what names, the theme, and the colours graphs are drawn in.
type settingsDialog struct {
	*qtwidgets.QDialog
	cpuInterval, memoryInterval, networkInterval, processesInterval *qtwidgets.QDoubleSpinBox
	cpuMode, theme, sizeUnits, rateUnits                            *qtwidgets.QComboBox
	// memoryMax is the top of the memory graph, in GB or GiB, and
	// defaultMemoryMax what it is unless another is chosen.
	memoryMax        *qtwidgets.QSpinBox
	defaultMemoryMax int
	// shown lists the network adapters, in the order of names, each ticked
	// if it is shown, with its alias beside it.
	shown *qtwidgets.QTableWidget
	// colourList is the table the swatches are listed in.
	colourList                *qtwidgets.QTableWidget
	grid, total, memory, swap *swatch
	cores                     []*swatch
	// adapters are the swatches of the network adapters, in the order of
	// names.
	adapters []*swatch
	names    []string
	// resetColours puts the default colours in the dialog.
	resetColours *qtwidgets.QPushButton
	box          *qtwidgets.QDialogButtonBox
}

// tabMargin is the space between the edge of a tab of the settings dialog
// and what is on it.
const tabMargin = 20

// settingsWidth is the narrowest the settings dialog opens, and
// settingsLeastWidth the narrowest it can be made.
const (
	settingsWidth      = 600
	settingsLeastWidth = 560
)

// colourTableHeight is the least height of the table of colours, which
// scrolls to show the rest.
const colourTableHeight = 300

// newSettingsDialog shows the settings, with a colour for each of the
// processors there are and of the network adapters named, and the top of the
// memory graph unless another is chosen, and calls accepted with them if they
// are.
func newSettingsDialog(parent qtwidgets.QWidgetLike, current settings.Settings, cores int, adapters []string, defaultMemoryMax int, accepted func(settings.Settings)) *settingsDialog {
	d := &settingsDialog{QDialog: qtwidgets.NewQDialog(parent), names: adapters, defaultMemoryMax: defaultMemoryMax}
	d.SetWindowTitle("Settings")
	d.SetMinimumWidth(settingsLeastWidth)
	tabs := qtwidgets.NewQTabWidget(nil)

	tabs.AddTab(plainPage(d.generalTab()), "General")
	tabs.AddTab(plainPage(d.networkTab()), "Network")
	tabs.AddTab(plainPage(d.interfaceTab(current, cores)), "User interface")

	d.show(current)

	d.box = qtwidgets.NewQDialogButtonBoxWithButtonsOriented(qtwidgets.QDialogButtonBoxOk|qtwidgets.QDialogButtonBoxCancel|qtwidgets.QDialogButtonBoxRestoreDefaults, qtcore.Horizontal, nil)
	d.box.Button(qtwidgets.QDialogButtonBoxRestoreDefaults).OnClicked(func(bool) {
		d.show(defaultsFor(current))
	})
	d.box.OnRejected(d.Reject)
	d.box.OnAccepted(func() {
		d.Accept()
		accepted(d.chosen(current))
	})

	layout := qtwidgets.NewQVBoxLayout(d)
	layout.AddWidgetFull(tabs, 1, 0)
	layout.AddWidget(d.box)
	// It opens as big as its tabs need, which some styles, as Breeze,
	// space more widely; smaller, their rows are squashed.
	fitDialog(d.QDialog, settingsWidth)
	d.OnFinished(func(int) { d.DeleteLater() })
	d.Open()
	return d
}

// generalTab makes the tab of how often each statistic is read, how the
// processors are shown, the top of the memory graph, and the units.
func (d *settingsDialog) generalTab() *qtwidgets.QWidget {
	general := qtwidgets.NewQWidget(nil)
	generalForm := qtwidgets.NewQFormLayout(general)
	generalForm.SetContentsMargins(qtcore.QMargins{Left: tabMargin, Top: tabMargin, Right: tabMargin, Bottom: tabMargin})
	d.cpuInterval = newIntervalBox()
	d.memoryInterval = newIntervalBox()
	d.networkInterval = newIntervalBox()
	d.processesInterval = newIntervalBox()
	generalForm.AddRowWidget(sectionHeading("Update intervals"))
	generalForm.AddRowText("CPU", d.cpuInterval)
	generalForm.AddRowText("Memory and swap", d.memoryInterval)
	generalForm.AddRowText("Network", d.networkInterval)
	generalForm.AddRowText("Processes", d.processesInterval)
	generalForm.AddRowWidget(note("How often each is read, from a tenth of a second to an hour. " +
		"A graph spans a minute, or two intervals where they are longer."))
	generalForm.AddRowWidget(laterHeading("Display"))
	d.cpuMode = qtwidgets.NewQComboBox(nil)
	addItem(d.cpuMode, "A line for each core", settings.CPUCores)
	addItem(d.cpuMode, "One line for all cores", settings.CPUTotal)
	generalForm.AddRowText("CPU", d.cpuMode)
	d.memoryMax = qtwidgets.NewQSpinBox(nil)
	d.memoryMax.SetRange(1, settings.MaxMemoryMax)
	generalForm.AddRowText("Maximum memory", d.memoryMax)
	generalForm.AddRowWidget(laterHeading("Units"))
	d.sizeUnits = qtwidgets.NewQComboBox(nil)
	addItem(d.sizeUnits, "Binary (KiB, MiB, GiB)", settings.SizeBinary)
	addItem(d.sizeUnits, "Decimal (kB, MB, GB)", settings.SizeDecimal)
	generalForm.AddRowText("Sizes", d.sizeUnits)
	d.sizeUnits.OnCurrentIndexChanged(func(int) { d.showMemoryUnit() })
	d.rateUnits = qtwidgets.NewQComboBox(nil)
	addItem(d.rateUnits, "Bytes per second (MiB/s, MB/s)", settings.RateBytes)
	addItem(d.rateUnits, "Bits per second (Mib/s, Mb/s)", settings.RateBits)
	generalForm.AddRowText("Network speed", d.rateUnits)
	return general
}

// networkTab makes the tab of the network adapters.
func (d *settingsDialog) networkTab() *qtwidgets.QWidget {
	network := qtwidgets.NewQWidget(nil)
	networkLayout := qtwidgets.NewQVBoxLayout(network)
	networkLayout.SetContentsMargins(qtcore.QMargins{Left: tabMargin, Top: tabMargin, Right: tabMargin, Bottom: tabMargin})
	networkLayout.AddWidget(sectionHeading("Adapters"))
	networkLayout.AddWidget(note("Those ticked are shown in the network graphs and legend, by their alias where they have one. " +
		"Every adapter that has been seen is listed, and keeps its choices for when it is seen again."))
	d.shown = d.adapterTable()
	networkLayout.AddWidgetFull(d.shown, 1, 0)
	return network
}

// interfaceTab makes the tab of the theme and the colours, whose defaults are
// worked out from the settings current.
func (d *settingsDialog) interfaceTab(current settings.Settings, cores int) *qtwidgets.QWidget {
	ui := qtwidgets.NewQWidget(nil)
	coloursLayout := qtwidgets.NewQVBoxLayout(ui)
	coloursLayout.SetContentsMargins(qtcore.QMargins{Left: tabMargin, Top: tabMargin, Right: tabMargin, Bottom: tabMargin})
	coloursLayout.AddWidget(sectionHeading("Appearance"))
	themeForm := qtwidgets.NewQFormLayout(nil)
	d.theme = qtwidgets.NewQComboBox(nil)
	for _, t := range themes {
		addItem(d.theme, t.label, t.name)
	}
	themeForm.AddRowText("Theme", d.theme)
	coloursLayout.AddLayout(themeForm)
	coloursLayout.AddWidget(laterHeading("Colours"))
	// The table has what height the tab has left, and scrolls.
	d.colourList = d.colourTable(cores)
	d.colourList.SetMinimumHeight(colourTableHeight)
	coloursLayout.AddWidgetFull(d.colourList, 1, 0)
	d.resetColours = qtwidgets.NewQPushButtonWithText("Use Default Colours", nil)
	d.resetColours.OnClicked(func(bool) { d.showColours(defaultsFor(current).Colours) })
	resetRow := qtwidgets.NewQHBoxLayout(nil)
	resetRow.AddStretch()
	resetRow.AddWidget(d.resetColours)
	coloursLayout.AddLayout(resetRow)
	return ui
}

// The columns of the table of network adapters.
const (
	colAdapterName = iota
	colAdapterAlias
)

// adapterTable lists the network adapters in a table: a row for each, with
// its name, ticked if it is shown, and its alias, which can be edited.
func (d *settingsDialog) adapterTable() *qtwidgets.QTableWidget {
	table := qtwidgets.NewQTableWidgetWithRows(0, 2, nil)
	table.SetHorizontalHeaderLabels([]string{"Adapter", "Alias"})
	table.VerticalHeader().SetVisible(false)
	table.SetSelectionMode(qtwidgets.QAbstractItemViewSingleSelection)
	table.SetHorizontalScrollBarPolicy(qtcore.ScrollBarAlwaysOff)
	header := table.HorizontalHeader()
	header.SetSectionResizeMode(colAdapterName, qtwidgets.QHeaderViewResizeToContents)
	header.SetSectionResizeMode(colAdapterAlias, qtwidgets.QHeaderViewStretch)
	for row, name := range d.names {
		table.InsertRow(row)
		item := qtwidgets.NewQTableWidgetItemWithText(name)
		item.SetFlags(qtcore.ItemIsEnabled | qtcore.ItemIsUserCheckable)
		table.SetItem(row, colAdapterName, item)
		alias := qtwidgets.NewQTableWidgetItemWithText("")
		alias.SetFlags(qtcore.ItemIsEnabled | qtcore.ItemIsSelectable | qtcore.ItemIsEditable)
		table.SetItem(row, colAdapterAlias, alias)
	}
	return table
}

// The columns of the table of colours.
const (
	colColourName = iota
	colColourSwatch
)

// colourTable lists every colour in a table, which scrolls: a row for each,
// with its name and its swatch, under a heading row for each section.
func (d *settingsDialog) colourTable(cores int) *qtwidgets.QTableWidget {
	table := qtwidgets.NewQTableWidgetWithRows(0, 2, nil)
	table.SetHorizontalHeaderLabels([]string{"Name", "Colour"})
	table.VerticalHeader().SetVisible(false)
	table.VerticalHeader().SetSectionResizeModeAll(qtwidgets.QHeaderViewResizeToContents)
	table.SetSelectionMode(qtwidgets.QAbstractItemViewNoSelection)
	table.SetEditTriggers(qtwidgets.QAbstractItemViewNoEditTriggers)
	table.SetFocusPolicy(qtcore.NoFocus)
	table.SetVerticalScrollBarPolicy(qtcore.ScrollBarAsNeeded)
	table.SetHorizontalScrollBarPolicy(qtcore.ScrollBarAlwaysOff)
	header := table.HorizontalHeader()
	header.SetSectionResizeMode(colColourName, qtwidgets.QHeaderViewStretch)
	header.SetSectionResizeMode(colColourSwatch, qtwidgets.QHeaderViewResizeToContents)

	heading := func(title string) {
		// A heading is as wide as the table.
		item := qtwidgets.NewQTableWidgetItemWithText(title)
		item.SetFlags(qtcore.ItemIsEnabled)
		font := item.Font()
		defer font.Delete()
		font.SetBold(true)
		item.SetFont(font)
		row := table.RowCount()
		table.InsertRow(row)
		table.SetItem(row, colColourName, item)
		table.SetSpan(row, colColourName, 1, 2)
	}
	add := func(name string) *swatch {
		s := newSwatch(d, name)
		item := qtwidgets.NewQTableWidgetItemWithText(name)
		item.SetFlags(qtcore.ItemIsEnabled)
		// The swatch stands a little clear of the lines of its cell.
		cell := qtwidgets.NewQWidget(nil)
		layout := qtwidgets.NewQHBoxLayout(cell)
		layout.SetContentsMargins(qtcore.QMargins{Left: 6, Top: 3, Right: 6, Bottom: 3})
		layout.AddWidget(s)
		row := table.RowCount()
		table.InsertRow(row)
		table.SetItem(row, colColourName, item)
		table.SetCellWidget(row, colColourSwatch, cell)
		return s
	}

	heading("Graphs")
	d.grid = add("Grid")
	d.memory = add("Memory")
	d.swap = add("Swap")
	heading("CPU")
	d.total = add("All cores")
	for i := range cores {
		d.cores = append(d.cores, add(fmt.Sprintf("CPU%d", i+1)))
	}
	if len(d.names) > 0 {
		heading("Network")
		for _, name := range d.names {
			d.adapters = append(d.adapters, add(name))
		}
	}
	return table
}

// defaultsFor returns the default settings, with the size of the window as
// it is, and with the network adapters there are given colours afresh.
func defaultsFor(current settings.Settings) settings.Settings {
	d := settings.Defaults()
	d.Width, d.Height, d.Maximized = current.Width, current.Height, current.Maximized
	for _, name := range sortedKeys(current.Colours.Network) {
		d.Colours.Adapter(name)
	}
	return d
}

// show puts the settings in the dialog.
func (d *settingsDialog) show(s settings.Settings) {
	d.cpuInterval.SetValue(s.Intervals.CPU)
	d.memoryInterval.SetValue(s.Intervals.Memory)
	d.networkInterval.SetValue(s.Intervals.Network)
	d.processesInterval.SetValue(s.Intervals.Processes)
	selectData(d.cpuMode, s.CPUMode)
	selectData(d.theme, s.Theme)
	selectData(d.sizeUnits, s.SizeUnits)
	d.showMemoryUnit()
	d.memoryMax.SetValue(d.defaultMemoryMax)
	if s.MemoryMax != 0 {
		d.memoryMax.SetValue(s.MemoryMax)
	}
	selectData(d.rateUnits, s.RateUnits)
	for i, name := range d.names {
		state := qtcore.Checked
		if !s.Shown(name) {
			state = qtcore.Unchecked
		}
		d.shown.Item(i, colAdapterName).SetCheckState(state)
		d.shown.Item(i, colAdapterAlias).SetText(s.AdapterAliases[name])
	}
	d.showColours(s.Colours)
}

// showMemoryUnit names the unit of the top of the memory graph, in the units
// of sizes chosen.
func (d *settingsDialog) showMemoryUnit() {
	if currentData(d.sizeUnits) == settings.SizeDecimal {
		d.memoryMax.SetSuffix(" GB")
	} else {
		d.memoryMax.SetSuffix(" GiB")
	}
}

// showColours puts the colours in the dialog.
func (d *settingsDialog) showColours(c settings.Colours) {
	d.grid.set(c.Grid)
	d.total.set(c.Total)
	d.memory.set(c.Memory)
	d.swap.set(c.Swap)
	for i, s := range d.cores {
		s.set(c.Core(i))
	}
	for i, s := range d.adapters {
		s.set(c.Network[d.names[i]])
	}
}

// chosen returns the settings as the dialog has them, with what it does not
// show as it is in current.
func (d *settingsDialog) chosen(current settings.Settings) settings.Settings {
	s := current
	s.Intervals = settings.Intervals{
		CPU:       d.cpuInterval.Value(),
		Memory:    d.memoryInterval.Value(),
		Network:   d.networkInterval.Value(),
		Processes: d.processesInterval.Value(),
	}
	s.CPUMode = currentData(d.cpuMode)
	s.Theme = currentData(d.theme)
	// The default is not kept as a choice, so that it follows the memory.
	s.MemoryMax = d.memoryMax.Value()
	if s.MemoryMax == d.defaultMemoryMax {
		s.MemoryMax = 0
	}
	s.SizeUnits = currentData(d.sizeUnits)
	s.RateUnits = currentData(d.rateUnits)

	// An adapter hidden that is not listed stays hidden.
	s.HiddenAdapters = nil
	for _, name := range current.HiddenAdapters {
		if !slices.Contains(d.names, name) {
			s.HiddenAdapters = append(s.HiddenAdapters, name)
		}
	}
	for i, name := range d.names {
		if d.shown.Item(i, colAdapterName).CheckState() != qtcore.Checked {
			s.HiddenAdapters = append(s.HiddenAdapters, name)
		}
	}
	slices.Sort(s.HiddenAdapters)

	// An adapter that is not listed keeps its alias.
	s.AdapterAliases = map[string]string{}
	for name, alias := range current.AdapterAliases {
		if !slices.Contains(d.names, name) {
			s.AdapterAliases[name] = alias
		}
	}
	for i, name := range d.names {
		if alias := strings.TrimSpace(d.shown.Item(i, colAdapterAlias).Text()); alias != "" {
			s.AdapterAliases[name] = alias
		}
	}
	if len(s.AdapterAliases) == 0 {
		s.AdapterAliases = nil
	}

	s.Colours = settings.Colours{
		Grid:    d.grid.colour,
		Total:   d.total.colour,
		Memory:  d.memory.colour,
		Swap:    d.swap.colour,
		Network: map[string]string{},
	}
	// Only the colours of the processors that are not their defaults are
	// kept, so that a processor keeps its default as long as it is not
	// changed. Those of processors beyond the ones listed, which another
	// computer the settings are used on can have, are kept as they are, as
	// those of adapters not listed are.
	for i, sw := range d.cores {
		if sw.colour != settings.DefaultCore(i) {
			s.Colours.SetCore(i, sw.colour)
		}
	}
	for i := len(d.cores); i < len(current.Colours.Cores); i++ {
		if colour := current.Colours.Cores[i]; colour != "" {
			s.Colours.SetCore(i, colour)
		}
	}
	maps.Copy(s.Colours.Network, current.Colours.Network)
	for i, sw := range d.adapters {
		s.Colours.Network[d.names[i]] = sw.colour
	}
	return s
}

// sectionHeading returns the heading of a section of a tab.
func sectionHeading(title string) *qtwidgets.QLabel {
	label := qtwidgets.NewQLabelWithText(title, nil)
	// The font is a copy of the label's.
	font := label.Font()
	defer font.Delete()
	font.SetBold(true)
	label.SetFont(font)
	return label
}

// laterHeading returns the heading of a section of a tab that follows another,
// with room above it.
func laterHeading(title string) *qtwidgets.QLabel {
	label := sectionHeading(title)
	label.SetContentsMargins(qtcore.QMargins{Left: 0, Top: 12, Right: 0, Bottom: 0})
	return label
}

// note returns a note on what a section of a tab is for, which wraps.
func note(text string) *qtwidgets.QLabel {
	label := qtwidgets.NewQLabelWithText(text, nil)
	label.SetWordWrap(true)
	label.SetEnabled(false)
	return label
}

// plainPage gives the page of a tab widget the background of the window:
// some styles draw the pages of a tab widget lighter than
// the window, which in a dark theme is a slab of grey. The page is returned.
func plainPage(page *qtwidgets.QWidget) *qtwidgets.QWidget {
	page.SetBackgroundRole(qtgui.QPaletteWindow)
	page.SetAutoFillBackground(true)
	return page
}

// newIntervalBox returns a box for an interval, in seconds to a tenth of a
// second, which its arrows change by half a second.
func newIntervalBox() *qtwidgets.QDoubleSpinBox {
	b := qtwidgets.NewQDoubleSpinBox(nil)
	b.SetDecimals(1)
	b.SetSingleStep(0.5)
	b.SetRange(settings.MinInterval, settings.MaxInterval)
	b.SetSuffix(" s")
	b.SetAccelerated(true)
	return b
}

// addItem adds an item to the box, with the data it stands for.
func addItem(box *qtwidgets.QComboBox, label, data string) {
	v := qtcore.NewQVariantOf(data)
	box.AddItemFull(label, v)
	v.Delete()
}

// selectData makes the item of the box with the data the current one.
func selectData(box *qtwidgets.QComboBox, data string) {
	for i := range box.Count() {
		v := box.ItemData(i)
		if v.ToString() == data {
			box.SetCurrentIndex(i)
		}
		v.Delete()
	}
}

// currentData returns the data of the current item of the box.
func currentData(box *qtwidgets.QComboBox) string {
	v := box.CurrentData()
	defer v.Delete()
	return v.ToString()
}

// swatchButtonWidth is the width of every swatch, so that they line up.
const swatchButtonWidth = 140

// swatch is a button filled with a colour, and named with it, which chooses
// another when it is clicked.
type swatch struct {
	*qtwidgets.QPushButton
	// colour is the colour, as the settings write it.
	colour string
}

// newSwatch returns a swatch that chooses the colour of what is named, in a
// dialog over the parent.
func newSwatch(parent qtwidgets.QWidgetLike, name string) *swatch {
	s := &swatch{QPushButton: qtwidgets.NewQPushButton(nil)}
	s.SetFixedWidth(swatchButtonWidth)
	s.OnClicked(func(bool) {
		if colour, ok := pickColour(parent, name, s.colour); ok {
			s.set(colour)
		}
	})
	return s
}

// pickColour asks for the colour of what is named, starting from the
// colour, in a dialog over the parent, and returns the colour chosen, as the
// settings write it, and false if none is. Tests replace it, as the dialog
// waits for someone to choose.
var pickColour = func(parent qtwidgets.QWidgetLike, name, colour string) (string, bool) {
	chosen := qtwidgets.QColorDialogGetColorFull(parseColour(colour), parent, "Colour of "+name, qtwidgets.QColorDialogShowAlphaChannel)
	if !chosen.IsValid() {
		return "", false
	}
	return colourName(chosen), true
}

// set fills the swatch with the colour, as the settings write it, and names
// it on it, in black or white, whichever can be read against it.
func (s *swatch) set(colour string) {
	s.colour = colour
	c := parseColour(colour)
	s.SetText(colour)
	s.SetStyleSheet(fmt.Sprintf(
		"QPushButton { background-color: rgba(%d, %d, %d, %d); color: %s; border: 1px solid #888888; border-radius: 4px; padding: 5px 0; }",
		c.R, c.G, c.B, c.A, readableOn(c),
	))
}
