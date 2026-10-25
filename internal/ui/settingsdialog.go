package ui

import (
	"fmt"
	"runtime"
	"slices"
	"strings"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/prism/internal/settings"
)

// settingsDialog shows the settings: how often each statistic is read, how
// the processors are shown, the units sizes and rates are written in, which
// network adapters are shown and by what names, the theme, and the colours
// graphs are drawn in.
type settingsDialog struct {
	*qt.QDialog
	cpuInterval, memoryInterval, networkInterval *qt.QSpinBox
	cpuMode, theme, sizeUnits, rateUnits         *qt.QComboBox
	// shown lists the network adapters, in the order of names, each ticked
	// if it is shown, with its alias beside it.
	shown *qt.QTableWidget
	// colourList is the table the swatches are listed in.
	colourList                *qt.QTableWidget
	grid, total, memory, swap *swatch
	cores                     []*swatch
	// adapters are the swatches of the network adapters, in the order of
	// names.
	adapters []*swatch
	names    []string
	// resetColours puts the default colours in the dialog.
	resetColours *qt.QPushButton
	box          *qt.QDialogButtonBox
}

// tabMargin is the space between the edge of a tab of the settings dialog
// and what is on it, as TriggerAPI has it.
const tabMargin = 20

// colourTableHeight is the least height of the table of colours, which
// scrolls to show the rest.
const colourTableHeight = 300

// newSettingsDialog shows the settings, with a colour for each of the
// processors there are and of the network adapters named, and calls accepted
// with them if they are.
func newSettingsDialog(parent *qt.QWidget, current settings.Settings, cores int, adapters []string, accepted func(settings.Settings)) *settingsDialog {
	d := &settingsDialog{QDialog: qt.NewQDialog(parent), names: adapters}
	d.SetWindowTitle("Settings")
	d.SetMinimumWidth(560)
	tabs := qt.NewQTabWidget2()

	general := qt.NewQWidget2()
	generalForm := qt.NewQFormLayout(general)
	generalForm.SetContentsMargins(tabMargin, tabMargin, tabMargin, tabMargin)
	d.cpuInterval = newIntervalBox()
	d.memoryInterval = newIntervalBox()
	d.networkInterval = newIntervalBox()
	generalForm.AddRowWithWidget(sectionHeading("Update intervals").QWidget)
	generalForm.AddRow3("CPU", d.cpuInterval.QWidget)
	generalForm.AddRow3("Memory and swap", d.memoryInterval.QWidget)
	generalForm.AddRow3("Network", d.networkInterval.QWidget)
	generalForm.AddRowWithWidget(note("How often each is read, from a second to an hour. " +
		"A graph spans a minute, or two intervals where they are longer.").QWidget)
	display := sectionHeading("Display")
	display.SetContentsMargins(0, 12, 0, 0)
	generalForm.AddRowWithWidget(display.QWidget)
	d.cpuMode = qt.NewQComboBox2()
	addItem(d.cpuMode, "A line for each core", settings.CPUCores)
	addItem(d.cpuMode, "One line for all cores", settings.CPUTotal)
	generalForm.AddRow3("CPU", d.cpuMode.QWidget)
	units := sectionHeading("Units")
	units.SetContentsMargins(0, 12, 0, 0)
	generalForm.AddRowWithWidget(units.QWidget)
	d.sizeUnits = qt.NewQComboBox2()
	addItem(d.sizeUnits, "Binary (KiB, MiB, GiB)", settings.SizeBinary)
	addItem(d.sizeUnits, "Decimal (kB, MB, GB)", settings.SizeDecimal)
	generalForm.AddRow3("Sizes", d.sizeUnits.QWidget)
	d.rateUnits = qt.NewQComboBox2()
	addItem(d.rateUnits, "Bytes per second (MiB/s, MB/s)", settings.RateBytes)
	addItem(d.rateUnits, "Bits per second (Mib/s, Mb/s)", settings.RateBits)
	generalForm.AddRow3("Network speed", d.rateUnits.QWidget)

	network := qt.NewQWidget2()
	networkLayout := qt.NewQVBoxLayout(network)
	networkLayout.SetContentsMargins(tabMargin, tabMargin, tabMargin, tabMargin)
	networkLayout.AddWidget(sectionHeading("Adapters").QWidget)
	networkLayout.AddWidget(note("Those ticked are shown in the network graphs and legend, by their alias where they have one. " +
		"Every adapter that has been seen is listed, and keeps its choices for when it is seen again.").QWidget)
	d.shown = d.adapterTable()
	networkLayout.AddWidget2(d.shown.QWidget, 1)

	ui := qt.NewQWidget2()
	coloursLayout := qt.NewQVBoxLayout(ui)
	coloursLayout.SetContentsMargins(tabMargin, tabMargin, tabMargin, tabMargin)
	coloursLayout.AddWidget(sectionHeading("Appearance").QWidget)
	themeForm := qt.NewQFormLayout2()
	d.theme = qt.NewQComboBox2()
	for _, t := range themes {
		addItem(d.theme, t.label, t.name)
	}
	themeForm.AddRow3("Theme", d.theme.QWidget)
	coloursLayout.AddLayout(themeForm.QLayout)
	colourHeading := sectionHeading("Colours")
	colourHeading.SetContentsMargins(0, 12, 0, 0)
	coloursLayout.AddWidget(colourHeading.QWidget)
	// The table has what height the tab has left, and scrolls.
	d.colourList = d.colourTable(cores)
	d.colourList.SetMinimumHeight(colourTableHeight)
	coloursLayout.AddWidget2(d.colourList.QWidget, 1)
	d.resetColours = qt.NewQPushButton3("Use Default Colours")
	d.resetColours.OnClicked(func() { d.showColours(defaultsFor(current).Colours) })
	resetRow := qt.NewQHBoxLayout2()
	resetRow.AddStretch()
	resetRow.AddWidget(d.resetColours.QWidget)
	coloursLayout.AddLayout(resetRow.QLayout)

	tabs.AddTab(plainPage(general), "General")
	tabs.AddTab(plainPage(network), "Network")
	tabs.AddTab(plainPage(ui), "User interface")

	d.show(current)

	d.box = qt.NewQDialogButtonBox5(qt.QDialogButtonBox__Ok|qt.QDialogButtonBox__Cancel|qt.QDialogButtonBox__RestoreDefaults, qt.Horizontal)
	d.box.Button(qt.QDialogButtonBox__RestoreDefaults).OnClicked(func() {
		d.show(defaultsFor(current))
	})
	d.box.OnRejected(d.Reject)
	d.box.OnAccepted(func() {
		d.Accept()
		accepted(d.chosen(current))
	})

	layout := qt.NewQVBoxLayout(d.QWidget)
	layout.AddWidget2(tabs.QWidget, 1)
	layout.AddWidget(d.box.QWidget)
	d.Resize(600, 560)
	d.OnFinished(func(int) { d.DeleteLater() })
	d.Open()
	return d
}

// The columns of the table of network adapters.
const (
	colAdapterName = iota
	colAdapterAlias
)

// adapterTable lists the network adapters in a table: a row for each, with
// its name, ticked if it is shown, and its alias, which can be edited.
func (d *settingsDialog) adapterTable() *qt.QTableWidget {
	table := qt.NewQTableWidget3(0, 2)
	table.SetHorizontalHeaderLabels([]string{"Adapter", "Alias"})
	table.VerticalHeader().SetVisible(false)
	table.SetSelectionMode(qt.QAbstractItemView__SingleSelection)
	table.SetHorizontalScrollBarPolicy(qt.ScrollBarAlwaysOff)
	header := table.HorizontalHeader()
	header.SetSectionResizeMode2(colAdapterName, qt.QHeaderView__ResizeToContents)
	header.SetSectionResizeMode2(colAdapterAlias, qt.QHeaderView__Stretch)
	for row, name := range d.names {
		table.InsertRow(row)
		item := qt.NewQTableWidgetItem2(name)
		item.SetFlags(qt.ItemIsEnabled | qt.ItemIsUserCheckable)
		table.SetItem(row, colAdapterName, item)
		alias := qt.NewQTableWidgetItem2("")
		alias.SetFlags(qt.ItemIsEnabled | qt.ItemIsSelectable | qt.ItemIsEditable)
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
// with its name and its swatch, under a heading row for each section, as
// TriggerAPI lists its colours.
func (d *settingsDialog) colourTable(cores int) *qt.QTableWidget {
	table := qt.NewQTableWidget3(0, 2)
	table.SetHorizontalHeaderLabels([]string{"Name", "Colour"})
	table.VerticalHeader().SetVisible(false)
	table.VerticalHeader().SetSectionResizeMode(qt.QHeaderView__ResizeToContents)
	table.SetSelectionMode(qt.QAbstractItemView__NoSelection)
	table.SetEditTriggers(qt.QAbstractItemView__NoEditTriggers)
	table.SetFocusPolicy(qt.NoFocus)
	table.SetVerticalScrollBarPolicy(qt.ScrollBarAsNeeded)
	table.SetHorizontalScrollBarPolicy(qt.ScrollBarAlwaysOff)
	header := table.HorizontalHeader()
	header.SetSectionResizeMode2(colColourName, qt.QHeaderView__Stretch)
	header.SetSectionResizeMode2(colColourSwatch, qt.QHeaderView__ResizeToContents)

	heading := func(title string) {
		// A heading is as wide as the table.
		item := qt.NewQTableWidgetItem2(title)
		item.SetFlags(qt.ItemIsEnabled)
		font := item.Font()
		font.SetBold(true)
		item.SetFont(font)
		row := table.RowCount()
		table.InsertRow(row)
		table.SetItem(row, colColourName, item)
		table.SetSpan(row, colColourName, 1, 2)
	}
	add := func(name string) *swatch {
		s := newSwatch(d.QWidget, name)
		item := qt.NewQTableWidgetItem2(name)
		item.SetFlags(qt.ItemIsEnabled)
		// The swatch stands a little clear of the lines of its cell.
		cell := qt.NewQWidget2()
		layout := qt.NewQHBoxLayout(cell)
		layout.SetContentsMargins(6, 3, 6, 3)
		layout.AddWidget(s.QWidget)
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
	selectData(d.cpuMode, s.CPUMode)
	selectData(d.theme, s.Theme)
	selectData(d.sizeUnits, s.SizeUnits)
	selectData(d.rateUnits, s.RateUnits)
	for i, name := range d.names {
		state := qt.Checked
		if !s.Shown(name) {
			state = qt.Unchecked
		}
		d.shown.Item(i, colAdapterName).SetCheckState(state)
		d.shown.Item(i, colAdapterAlias).SetText(s.AdapterAliases[name])
	}
	d.showColours(s.Colours)
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
		CPU:     d.cpuInterval.Value(),
		Memory:  d.memoryInterval.Value(),
		Network: d.networkInterval.Value(),
	}
	s.CPUMode = currentData(d.cpuMode)
	s.Theme = currentData(d.theme)
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
		if d.shown.Item(i, colAdapterName).CheckState() != qt.Checked {
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
	for name, colour := range current.Colours.Network {
		s.Colours.Network[name] = colour
	}
	for i, sw := range d.adapters {
		s.Colours.Network[d.names[i]] = sw.colour
	}
	return s
}

// sectionHeading returns the heading of a section of a tab.
func sectionHeading(title string) *qt.QLabel {
	label := qt.NewQLabel3(title)
	font := qt.NewQFont5(label.Font())
	defer font.Delete()
	font.SetBold(true)
	label.SetFont(font)
	return label
}

// note returns a note on what a section of a tab is for, which wraps.
func note(text string) *qt.QLabel {
	label := qt.NewQLabel3(text)
	label.SetWordWrap(true)
	label.SetEnabled(false)
	return label
}

// plainPage gives the page of a tab widget the background of the window, as
// TriggerAPI does: some styles draw the pages of a tab widget lighter than
// the window, which in a dark theme is a slab of grey. The page is returned.
func plainPage(page *qt.QWidget) *qt.QWidget {
	page.SetBackgroundRole(qt.QPalette__Window)
	page.SetAutoFillBackground(true)
	return page
}

// newIntervalBox returns a box for an interval, in seconds.
func newIntervalBox() *qt.QSpinBox {
	b := qt.NewQSpinBox2()
	b.SetRange(settings.MinInterval, settings.MaxInterval)
	b.SetSuffix(" s")
	b.SetAccelerated(true)
	return b
}

// addItem adds an item to the box, with the data it stands for.
func addItem(box *qt.QComboBox, label, data string) {
	v := qt.NewQVariant11(data)
	box.AddItem3(label, v)
	v.Delete()
}

// selectData makes the item of the box with the data the current one.
func selectData(box *qt.QComboBox, data string) {
	for i := range box.Count() {
		v := box.ItemData(i)
		if v.ToString() == data {
			box.SetCurrentIndex(i)
		}
		runtime.KeepAlive(v)
	}
}

// currentData returns the data of the current item of the box.
func currentData(box *qt.QComboBox) string {
	v := box.CurrentData()
	defer runtime.KeepAlive(v)
	return v.ToString()
}

// swatchButtonWidth is the width of every swatch, so that they line up.
const swatchButtonWidth = 140

// swatch is a button filled with a colour, and named with it, which chooses
// another when it is clicked, as TriggerAPI's are.
type swatch struct {
	*qt.QPushButton
	// colour is the colour, as the settings write it.
	colour string
}

// newSwatch returns a swatch that chooses the colour of what is named, in a
// dialog over the parent.
func newSwatch(parent *qt.QWidget, name string) *swatch {
	s := &swatch{QPushButton: qt.NewQPushButton2()}
	s.SetFixedWidth(swatchButtonWidth)
	s.OnClicked(func() {
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
var pickColour = func(parent *qt.QWidget, name, colour string) (string, bool) {
	initial := qt.NewQColor6(colour)
	defer initial.Delete()
	// The colour chosen is freed by the garbage collector.
	chosen := qt.QColorDialog_GetColor4(initial, parent, "Colour of "+name, qt.QColorDialog__ShowAlphaChannel)
	if !chosen.IsValid() {
		return "", false
	}
	return colourName(chosen), true
}

// set fills the swatch with the colour, as the settings write it, and names
// it on it, in black or white, whichever can be read against it.
func (s *swatch) set(colour string) {
	s.colour = colour
	c := qt.NewQColor6(colour)
	defer c.Delete()
	s.SetText(colour)
	s.SetStyleSheet(fmt.Sprintf(
		"QPushButton { background-color: rgba(%d, %d, %d, %d); color: %s; border: 1px solid #888888; border-radius: 4px; padding: 5px 0; }",
		c.Red(), c.Green(), c.Blue(), c.Alpha(), readableOn(c),
	))
}

// readableOn returns black or white, whichever can be read on the colour.
func readableOn(colour *qt.QColor) string {
	if colour.Lightness() > 140 {
		return "#000000"
	}
	return "#ffffff"
}

// colourName writes a colour as the settings do: #rrggbb where it is opaque,
// and #aarrggbb where it is not.
func colourName(c *qt.QColor) string {
	if c.Alpha() == 255 {
		return strings.ToLower(c.NameWithFormat(qt.QColor__HexRgb))
	}
	return strings.ToLower(c.NameWithFormat(qt.QColor__HexArgb))
}
