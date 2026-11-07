package ui

import (
	"cmp"
	"fmt"
	"maps"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"unsafe"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/prism/internal/chart"
	"github.com/nomad-software/prism/internal/settings"
	"github.com/nomad-software/prism/internal/stats"
	"github.com/nomad-software/prism/internal/usage"
)

// The columns of the processes.
const (
	colProcessPID = iota
	colProcessName
	colProcessCPU
	colProcessCores
	colProcessMemory
	processColumns
)

// processHeadings are the headings of the columns of the processes.
var processHeadings = [processColumns]string{"PID", "Name", "CPU", "Cores Used", "Memory"}

// processSorts are the settings' names of the columns of the processes, which
// they are sorted by.
var processSorts = [processColumns]string{
	colProcessPID:    settings.SortPID,
	colProcessName:   settings.SortName,
	colProcessCPU:    settings.SortCPU,
	colProcessCores:  settings.SortCores,
	colProcessMemory: settings.SortMemory,
}

// processShares are the shares of the width of the tree of processes, beside
// the PID, each other column has: the name two fifths, and the others a
// fifth.
var processShares = [processColumns]int{colProcessName: 2, colProcessCPU: 1, colProcessCores: 1, colProcessMemory: 1}

// columnPadding is the room either side of the PIDs, and sortArrowRoom that
// of the arrow beside the heading the tree is sorted by.
const (
	columnPadding = 24
	sortArrowRoom = 20
)

// processes is the tab of the processes: a tree of them, which can be sorted
// by any column, shown as a list or as a tree whose branches fold, or a
// picture of them, as a skyline or as territories, of every process, those
// of people or the system's, and what sends the one chosen a signal.
type processes struct {
	section
	usage usage.Processes
	// list is every process read, and shown those the tree lists, in its
	// order from the top, each before those it started.
	list  []usage.Process
	shown []usage.Process
	tree  *qt.QTreeWidget
	// pages has the tree, the skyline and the territories, of which the one
	// the view chooses is shown.
	pages       *qt.QStackedWidget
	skyline     *screen
	territories *screen
	// rows are the indexes in shown of the processes of the items of the
	// tree, by the items' addresses.
	rows map[unsafe.Pointer]int
	// folded are the processes whose branches are folded; the others are
	// open, as each is at first.
	folded map[stats.ProcessKey]bool
	// search narrows the processes shown to those whose names or PIDs hold
	// what is written in it.
	search *qt.QLineEdit
	// filter chooses which processes are shown, and view whether as a
	// list, a tree, a skyline or territories.
	filter *qt.QComboBox
	view   iconChoice
	// signals lists stats.Signals, by their names, and send sends the one
	// chosen to the process chosen.
	signals *qt.QComboBox
	send    *qt.QPushButton
	// chosen is the process chosen in the tree, which is kept chosen as
	// the tree is sorted and read again, while it runs.
	chosen *stats.Process
	// filling says that the tree is being filled, while which what Qt says
	// is chosen, folded or open is not what the user chose.
	filling bool
}

// makeProcesses makes the tab of the processes, sorted, chosen and shown as
// they last were.
func (w *Window) makeProcesses() {
	p := &w.processes
	p.section = newSection("Processes")
	// Which processes are shown, and how, are chosen below the tree, at its
	// left, and kept in the settings.
	p.filter = qt.NewQComboBox2()
	addItem(p.filter, "All", settings.ProcessesAll)
	addItem(p.filter, "User", settings.ProcessesUser)
	addItem(p.filter, "System", settings.ProcessesSystem)
	selectData(p.filter, w.config.ProcessFilter)
	p.view = newIconChoice(p.QObject, []iconOption{
		{"List", settings.ViewList, drawProcessesIcon},
		{"Tree", settings.ViewTree, drawTreeIcon},
		{"Skyline", settings.ViewSkyline, drawSkylineIcon},
		{"Territories", settings.ViewTerritories, drawTerritoriesIcon},
	})
	p.view.choose(w.config.ProcessView)
	p.filter.OnCurrentIndexChanged(func(int) { w.processViewChanged() })
	p.view.onChanged(w.processViewChanged)

	// What is searched for is written above the tree, and narrows it as it
	// is written. It is not kept.
	p.search = qt.NewQLineEdit2()
	p.search.SetPlaceholderText("Search by name or PID")
	p.search.SetClearButtonEnabled(true)
	p.search.OnTextChanged(func(string) { w.showProcesses() })
	p.layout.AddWidget(p.search.QWidget)

	w.makeProcessTree()
	p.skyline = newSkyline()
	p.territories = newTerritories()
	p.pages = qt.NewQStackedWidget2()
	for _, page := range []*qt.QWidget{p.tree.QWidget, p.skyline.QWidget, p.territories.QWidget} {
		p.pages.AddWidget(page)
	}
	for _, s := range []*screen{p.skyline, p.territories} {
		s.choose = w.chooseShown
	}
	p.showPage(p.picture(w.config.ProcessView))
	p.layout.AddWidget2(p.pages.QWidget, 1)
	p.layout.AddLayout(w.processControls().QLayout)
}

// makeProcessTree makes the tree of the processes, sorted as they last were.
func (w *Window) makeProcessTree() {
	p := &w.processes
	p.folded = map[stats.ProcessKey]bool{}
	p.tree = qt.NewQTreeWidget2()
	p.tree.SetColumnCount(processColumns)
	p.tree.SetSelectionMode(qt.QAbstractItemView__SingleSelection)
	p.tree.SetUniformRowHeights(true)
	p.tree.SetAllColumnsShowFocus(true)
	// The branches are drawn beside the names, though the PIDs are first.
	p.tree.SetTreePosition(colProcessName)
	headings := p.tree.HeaderItem()
	for col, heading := range processHeadings {
		headings.SetText(col, heading)
		headings.SetTextAlignment(col, int(processAlignment(col)))
		if col != colProcessName {
			// A column of numbers is sorted from the largest when it is
			// first chosen, and one of names from A.
			order := qt.NewQVariant4(int(qt.DescendingOrder))
			headings.SetData(col, int(qt.InitialSortOrderRole), order)
			order.Delete()
		}
	}
	header := p.tree.Header()
	// The columns share the width of the tree, as fitColumns says, as it
	// grows and shrinks. They are not sized to what they hold, as Qt
	// would measure every row whenever any of them changed. The last has
	// what is left, so that they always fill the width.
	for col := range processColumns {
		header.SetSectionResizeMode2(col, qt.QHeaderView__Fixed)
	}
	header.SetStretchLastSection(true)
	p.tree.OnResizeEvent(func(super func(*qt.QResizeEvent), event *qt.QResizeEvent) {
		super(event)
		p.fitColumns()
	})
	// The tree is sorted here, not by Qt, so that the numbers are sorted
	// as numbers, the process chosen stays chosen, and those each process
	// started stay under it. It is sorted as the settings say, and how it
	// is sorted by clicking a heading is saved.
	header.SetSectionsClickable(true)
	header.SetSortIndicatorShown(true)
	w.applyProcessOrder()
	header.OnSortIndicatorChanged(w.processOrderChanged)
	p.tree.OnItemSelectionChanged(w.chooseProcess)
	p.tree.OnItemCollapsed(func(item *qt.QTreeWidgetItem) { p.fold(item, true) })
	p.tree.OnItemExpanded(func(item *qt.QTreeWidgetItem) { p.fold(item, false) })
}

// processControls returns the row below the tree: at its left which processes
// are shown and how, and at its right what sends the one chosen a signal.
func (w *Window) processControls() *qt.QHBoxLayout {
	p := &w.processes
	p.signals = qt.NewQComboBox2()
	for _, s := range stats.Signals() {
		p.signals.AddItem(fmt.Sprintf("%s (%d)", s.Name, s.Number))
	}
	p.send = qt.NewQPushButton3("Send Signal")
	p.enableSend()
	p.send.OnClicked(w.sendSignal)
	row := qt.NewQHBoxLayout2()
	row.AddWidget(qt.NewQLabel3("Processes").QWidget)
	row.AddWidget(p.filter.QWidget)
	row.AddSpacing(12)
	row.AddWidget(qt.NewQLabel3("Show as").QWidget)
	for _, b := range p.view.buttons {
		row.AddWidget(b.QWidget)
	}
	row.AddStretch()
	row.AddWidget(qt.NewQLabel3("Signal").QWidget)
	row.AddWidget(p.signals.QWidget)
	row.AddWidget(p.send.QWidget)
	return row
}

// processAlignment returns how the column of the processes, and its
// heading, are aligned: the PIDs and names to the left, and the use of the
// processor and memory to the right.
func processAlignment(col int) qt.AlignmentFlag {
	if col == colProcessPID || col == colProcessName {
		return qt.AlignLeft | qt.AlignVCenter
	}
	return qt.AlignRight | qt.AlignVCenter
}

// applyProcessOrder sorts the processes as the settings say, as they were
// last sorted.
func (w *Window) applyProcessOrder() {
	col := max(slices.Index(processSorts[:], w.config.ProcessSort), 0)
	order := qt.AscendingOrder
	if w.config.ProcessOrder == settings.OrderDescending {
		order = qt.DescendingOrder
	}
	w.processes.tree.Header().SetSortIndicator(col, order)
}

// processOrderChanged sorts the processes by the column, as its
// heading was clicked, and saves how it is sorted in the settings.
func (w *Window) processOrderChanged(col int, order qt.SortOrder) {
	if col >= 0 && col < processColumns {
		sort, dir := processSorts[col], settings.OrderAscending
		if order == qt.DescendingOrder {
			dir = settings.OrderDescending
		}
		if w.config.ProcessSort != sort || w.config.ProcessOrder != dir {
			w.config.ProcessSort, w.config.ProcessOrder = sort, dir
			w.saveConfig()
		}
	}
	w.showProcesses()
}

// processViewChanged shows the processes chosen, as they are chosen to be
// shown, and saves the choice in the settings.
func (w *Window) processViewChanged() {
	filter, view := currentData(w.processes.filter), w.processes.view.current()
	if w.config.ProcessFilter != filter || w.config.ProcessView != view {
		w.config.ProcessFilter, w.config.ProcessView = filter, view
		w.saveConfig()
	}
	w.showProcesses()
}

// fitColumns makes the PIDs as wide as the largest of them, and gives each
// other column its share of the rest of the width of the header, which is
// that of the tree less its scroll bar, when the tree is resized or filled.
// The PIDs are measured by how many digits the largest has, not by Qt, which
// would measure every row.
func (p *processes) fitColumns() {
	header := p.tree.Header()
	largest := 0
	for _, proc := range p.shown {
		largest = max(largest, proc.PID)
	}
	digits := strings.Repeat("8", len(strconv.Itoa(largest)))
	rowMetrics, headerMetrics := p.tree.FontMetrics(), header.FontMetrics()
	defer runtime.KeepAlive(rowMetrics)
	defer runtime.KeepAlive(headerMetrics)
	pid := max(rowMetrics.HorizontalAdvance(digits),
		headerMetrics.HorizontalAdvance(processHeadings[colProcessPID])+sortArrowRoom) + columnPadding
	header.ResizeSection(colProcessPID, pid)
	rest := max(header.Width()-pid, 0)
	total := 0
	for _, share := range processShares {
		total += share
	}
	for col := colProcessPID + 1; col < processColumns-1; col++ {
		header.ResizeSection(col, rest*processShares[col]/total)
	}
}

// nameProcessors writes in the heading of the column of how busy each process
// is how many processors there are, each of which is 100% of it: "CPU (8
// cores)". It is "CPU" while that is not known.
func (p *processes) nameProcessors(n int) {
	heading := processHeadings[colProcessCPU]
	switch {
	case n == 1:
		heading += " (1 core)"
	case n > 1:
		heading += fmt.Sprintf(" (%d cores)", n)
	}
	p.tree.HeaderItem().SetText(colProcessCPU, heading)
}

// sampleProcesses reads the processes, and lists them with how busy each was
// since they were last read, which is known from a process's second reading.
func (w *Window) sampleProcesses() {
	list, err := w.source.Processes()
	w.processes.report(err)
	if err != nil {
		return
	}
	w.processes.list = w.processes.usage.Update(w.now(), list, w.cpu.processors)
	w.showProcesses()
}

// showProcesses shows the processes the settings choose as they choose: in
// the tree, or as a picture.
func (w *Window) showProcesses() {
	p := &w.processes
	p.shown = filterProcesses(p.list, w.config.ProcessFilter, p.search.Text())
	pictured := p.picture(w.config.ProcessView)
	p.showPage(pictured)
	if pictured == nil {
		w.fillProcessTree()
		return
	}
	// The tree is emptied while it is not shown, as it would list processes
	// that are not those shown, and filled again when it is.
	p.filling = true
	p.tree.Clear()
	p.rows = nil
	p.filling = false
	if p.chosen != nil && !slices.ContainsFunc(p.shown, func(proc usage.Process) bool { return proc.Key() == p.chosen.Key() }) {
		p.chosen = nil
	}
	pictured.show(w.now(), p.shown, w.units(), p.chosenKey())
	p.enableSend()
}

// picture returns the screen of the view, or nil for the list and the tree.
func (p *processes) picture(view string) *screen {
	switch view {
	case settings.ViewSkyline:
		return p.skyline
	case settings.ViewTerritories:
		return p.territories
	}
	return nil
}

// showPage shows the picture, or the tree if it is nil.
func (p *processes) showPage(pictured *screen) {
	if pictured == nil {
		p.pages.SetCurrentWidget(p.tree.QWidget)
	} else {
		p.pages.SetCurrentWidget(pictured.QWidget)
	}
}

// chosenKey returns the key of the process chosen, or nil.
func (p *processes) chosenKey() *stats.ProcessKey {
	if p.chosen == nil {
		return nil
	}
	key := p.chosen.Key()
	return &key
}

// chooseShown chooses the process of the index in those shown, as one is
// clicked in a picture of them, or none for -1, and lets it be sent a
// signal.
func (w *Window) chooseShown(i int) {
	p := &w.processes
	p.chosen = nil
	if i >= 0 && i < len(p.shown) {
		proc := p.shown[i].Process
		p.chosen = &proc
	}
	for _, s := range []*screen{p.skyline, p.territories} {
		s.setChosen(p.chosenKey())
	}
	p.enableSend()
}

// fillProcessTree lists the processes shown in the tree, as a list or a
// tree, sorted as its header says. The tree is made again each time, with
// nothing drawn until it is done, and with the branches folded that were,
// the process chosen still chosen if it is still listed, and scrolled as it
// was.
func (w *Window) fillProcessTree() {
	p := &w.processes
	p.filling = true
	defer func() { p.filling = false }()
	header := p.tree.Header()
	column, descending := header.SortIndicatorSection(), header.SortIndicatorOrder() == qt.DescendingOrder
	tree := w.config.ProcessView == settings.ViewTree
	var depths []int
	if tree {
		p.shown, depths = treeOrder(p.shown, column, descending)
	} else {
		sortProcesses(p.shown, column, descending)
		depths = make([]int, len(p.shown))
	}

	scroll := p.tree.VerticalScrollBar().Value()
	p.tree.SetUpdatesEnabled(false)
	defer p.tree.SetUpdatesEnabled(true)
	p.tree.Clear()
	p.tree.SetRootIsDecorated(tree)
	p.tree.SetItemsExpandable(tree)
	p.rows = make(map[unsafe.Pointer]int, len(p.shown))
	items := make([]*qt.QTreeWidgetItem, len(p.shown))
	// parents are the items the last item is under, from the top.
	var parents []*qt.QTreeWidgetItem
	var chosen *qt.QTreeWidgetItem
	u := w.units()
	for i, proc := range p.shown {
		cells := processCells(proc, u)
		item := qt.NewQTreeWidgetItem2(cells[:])
		for col := range processColumns {
			item.SetTextAlignment(col, int(processAlignment(col)))
		}
		depth := min(depths[i], len(parents))
		parents = parents[:depth]
		if depth == 0 {
			p.tree.AddTopLevelItem(item)
		} else {
			parents[depth-1].AddChild(item)
		}
		parents = append(parents, item)
		items[i] = item
		p.rows[item.UnsafePointer()] = i
		if p.chosen != nil && proc.Key() == p.chosen.Key() {
			chosen = item
		}
	}
	if tree {
		p.tree.ExpandAll()
		running := make(map[stats.ProcessKey]bool, len(p.list))
		for _, proc := range p.list {
			running[proc.Key()] = true
		}
		// What is kept of processes that have ended is forgotten.
		maps.DeleteFunc(p.folded, func(key stats.ProcessKey, _ bool) bool { return !running[key] })
		for i, proc := range p.shown {
			if p.folded[proc.Key()] {
				items[i].SetExpanded(false)
			}
		}
	}
	if chosen != nil {
		p.tree.SetCurrentItem(chosen)
	} else {
		p.chosen = nil
	}
	p.tree.VerticalScrollBar().SetValue(scroll)
	p.enableSend()
	// The rows can have brought a scroll bar, which the header is narrowed
	// by, or a larger PID.
	p.fitColumns()
}

// fold keeps whether the branch of the process of the item is folded, as it
// was folded or opened.
func (p *processes) fold(item *qt.QTreeWidgetItem, folded bool) {
	if p.filling {
		return
	}
	i, ok := p.rows[item.UnsafePointer()]
	if !ok {
		return
	}
	key := p.shown[i].Key()
	if folded {
		p.folded[key] = true
	} else {
		delete(p.folded, key)
	}
}

// processCells writes what the tree lists of the process: its name, PID,
// how busy it was, and how many processors' worth of time that is ("—" while
// it is not known), and its memory, which is "—" for a process the system
// does not let it be read of, and for a thread of the kernel, which has none
// of its own.
func processCells(p usage.Process, u chart.Units) [processColumns]string {
	cells := [processColumns]string{strconv.Itoa(p.PID), p.Name, "—", "—", "—"}
	if p.Measured {
		cells[colProcessCPU] = chart.FormatPercent(p.Busy)
		cells[colProcessCores] = strconv.FormatFloat(p.Busy/100, 'f', 1, 64)
	}
	if p.MemoryKnown() {
		cells[colProcessMemory] = u.Size(float64(p.Memory))
	}
	return cells
}

// sortProcesses sorts the processes by the column, from the largest if
// descending; those of the same are sorted by PID. Those whose value is not
// known are taken to be less than any that is.
func sortProcesses(list []usage.Process, column int, descending bool) {
	known := func(ok bool, v float64) float64 {
		if !ok {
			return -1
		}
		return v
	}
	slices.SortStableFunc(list, func(a, b usage.Process) int {
		var c int
		switch column {
		case colProcessName:
			c = cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		case colProcessCPU, colProcessCores:
			// The cores used are how busy it was, in processors' worth.
			c = cmp.Compare(known(a.Measured, a.Busy), known(b.Measured, b.Busy))
		case colProcessMemory:
			c = cmp.Compare(known(a.MemoryKnown(), float64(a.Memory)), known(b.MemoryKnown(), float64(b.Memory)))
		}
		if descending {
			c = -c
		}
		if c == 0 {
			c = cmp.Compare(a.PID, b.PID)
			if descending && column == colProcessPID {
				c = -c
			}
		}
		return c
	})
}

// filterProcesses returns those of the processes the filter, one of the
// settings' Processes, chooses: every one, those of people, or the system's;
// and of those, the ones whose names hold what is searched for, whatever its
// case, or whose PIDs do, unless it is blank.
func filterProcesses(list []usage.Process, filter, search string) []usage.Process {
	search = strings.ToLower(strings.TrimSpace(search))
	return slices.DeleteFunc(slices.Clone(list), func(p usage.Process) bool {
		switch {
		case filter == settings.ProcessesUser && p.System(),
			filter == settings.ProcessesSystem && !p.System():
			return true
		case search == "":
			return false
		}
		return !strings.Contains(strings.ToLower(p.Name), search) && !strings.Contains(strconv.Itoa(p.PID), search)
	})
}

// treeOrder returns the processes as a tree, sorted as sortProcesses sorts
// them: each followed by those it started, among those listed, and those by
// theirs, and how deep in the tree each is. A process whose parent is not
// listed is at the top.
func treeOrder(list []usage.Process, column int, descending bool) ([]usage.Process, []int) {
	sortProcesses(list, column, descending)
	listed := make(map[int]bool, len(list))
	for _, p := range list {
		listed[p.PID] = true
	}
	children := map[int][]int{}
	var tops []int
	for i, p := range list {
		if p.PPID != p.PID && listed[p.PPID] {
			children[p.PPID] = append(children[p.PPID], i)
		} else {
			tops = append(tops, i)
		}
	}
	ordered := make([]usage.Process, 0, len(list))
	depths := make([]int, 0, len(list))
	done := make([]bool, len(list))
	var walk func(i, depth int)
	walk = func(i, depth int) {
		if done[i] {
			return
		}
		done[i] = true
		ordered = append(ordered, list[i])
		depths = append(depths, depth)
		for _, child := range children[list[i].PID] {
			walk(child, depth+1)
		}
	}
	for _, i := range tops {
		walk(i, 0)
	}
	// Processes that started each other in a ring, as the system would not
	// have them, are put at the top, so that none is left out.
	for i := range list {
		walk(i, 0)
	}
	return ordered, depths
}

// chooseProcess keeps which process is chosen in the tree, and lets it be
// sent a signal.
func (w *Window) chooseProcess() {
	p := &w.processes
	if p.filling {
		return
	}
	p.chosen = nil
	if items := p.tree.SelectedItems(); len(items) > 0 {
		if i, ok := p.rows[items[0].UnsafePointer()]; ok {
			proc := p.shown[i].Process
			p.chosen = &proc
		}
	}
	p.enableSend()
}

// enableSend lets a signal be sent while a process is chosen.
func (p *processes) enableSend() {
	p.send.SetEnabled(p.chosen != nil)
}

// sendSignal sends the signal chosen to the process chosen, and says in the
// status bar whether it was sent.
func (w *Window) sendSignal() {
	p := &w.processes
	signals := stats.Signals()
	i := p.signals.CurrentIndex()
	if p.chosen == nil || i < 0 || i >= len(signals) {
		return
	}
	s, proc := signals[i], *p.chosen
	what := fmt.Sprintf("%s (%d)", proc.Name, proc.PID)
	if err := w.source.Signal(proc, s.Number); err != nil {
		w.StatusBar().ShowMessage(fmt.Sprintf("%s could not be sent to %s: %v", s.Name, what, err))
		return
	}
	w.StatusBar().ShowMessage(fmt.Sprintf("Sent %s to %s", s.Name, what))
}

// iconOption is one of the choices of an iconChoice: its name, shown when
// the pointer rests on its button, the data it stands for, and how its icon
// is drawn.
type iconOption struct {
	name, data string
	draw       func(*qt.QPainter)
}

// iconChoice is a row of buttons with icons, of which one is chosen at a
// time, as the items of a dropdown are.
type iconChoice struct {
	options []iconOption
	buttons []*qt.QToolButton
}

// newIconChoice makes a button for each option, the first chosen, in a
// group kept by the parent.
func newIconChoice(parent *qt.QObject, options []iconOption) iconChoice {
	c := iconChoice{options: options}
	group := qt.NewQButtonGroup2(parent)
	for i, o := range options {
		b := qt.NewQToolButton2()
		b.SetCheckable(true)
		b.SetChecked(i == 0)
		b.SetToolTip(o.name)
		b.SetAccessibleName(o.name)
		group.AddButton(b.QAbstractButton)
		c.buttons = append(c.buttons, b)
	}
	return c
}

// choose checks the button of the option with the data, if there is one.
func (c iconChoice) choose(data string) {
	for i, o := range c.options {
		if o.data == data {
			c.buttons[i].SetChecked(true)
		}
	}
}

// current returns the data of the option chosen.
func (c iconChoice) current() string {
	for i, b := range c.buttons {
		if b.IsChecked() {
			return c.options[i].data
		}
	}
	return ""
}

// onChanged calls changed whenever another option is chosen.
func (c iconChoice) onChanged(changed func()) {
	for _, b := range c.buttons {
		b.OnToggled(func(checked bool) {
			if checked {
				changed()
			}
		})
	}
}

// drawIcons draws the icon of each button in the colour, at the ratio of
// pixels.
func (c iconChoice) drawIcons(colour *qt.QColor, ratio float64) {
	for i, o := range c.options {
		icon := paintedIcon(colour, ratio, o.draw)
		c.buttons[i].SetIcon(icon)
		icon.Delete()
	}
}
