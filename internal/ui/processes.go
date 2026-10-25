package ui

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	qt "github.com/mappu/miqt/qt6"

	"github.com/nomad-software/prism/internal/chart"
	"github.com/nomad-software/prism/internal/settings"
	"github.com/nomad-software/prism/internal/stats"
	"github.com/nomad-software/prism/internal/usage"
)

// The columns of the table of processes.
const (
	colProcessPID = iota
	colProcessName
	colProcessCPU
	colProcessMemory
	processColumns
)

// processHeadings are the headings of the columns of the table of processes.
var processHeadings = [processColumns]string{"PID", "Name", "CPU", "Memory"}

// processSorts are the settings' names of the columns of the table of
// processes, which they are sorted by.
var processSorts = [processColumns]string{
	colProcessPID:    settings.SortPID,
	colProcessName:   settings.SortName,
	colProcessCPU:    settings.SortCPU,
	colProcessMemory: settings.SortMemory,
}

// processShares are the shares of the width of the table of processes, beside
// the PID, each other column has: the name half, and the others a quarter.
var processShares = [processColumns]int{colProcessName: 2, colProcessCPU: 1, colProcessMemory: 1}

// columnPadding is the room either side of the PIDs, and sortArrowRoom that
// of the arrow beside the heading the table is sorted by.
const (
	columnPadding = 24
	sortArrowRoom = 20
)

// processes is the tab of the processes: a table of them, which can be sorted
// by any column, shown as a list or a tree, of every process, those of people
// or the system's, and what sends the one chosen a signal.
type processes struct {
	section
	usage usage.Processes
	// list is every process read, and shown those the table lists, in its
	// order, with how deep in the tree each is.
	list   []usage.Process
	shown  []usage.Process
	depths []int
	table  *qt.QTableWidget
	// filter chooses which processes are shown, and view whether as a list
	// or a tree.
	filter, view *qt.QComboBox
	// signals lists stats.Signals, by their names, and send sends the one
	// chosen to the process chosen.
	signals *qt.QComboBox
	send    *qt.QPushButton
	// chosen is the process chosen in the table, which is kept chosen as
	// the table is sorted and read again, while it runs.
	chosen *stats.Process
	// filling says that the table is being filled, while which what Qt
	// says is chosen is not what the user chose.
	filling bool
}

// makeProcesses makes the tab of the processes, listed by how busy they are,
// the busiest first.
func (w *Window) makeProcesses() {
	p := &w.processes
	p.section = newSection("Processes")
	// Which processes are shown, and how, are chosen below the table, at its
	// left, and kept in the settings.
	p.filter = qt.NewQComboBox2()
	addItem(p.filter, "All", settings.ProcessesAll)
	addItem(p.filter, "User", settings.ProcessesUser)
	addItem(p.filter, "System", settings.ProcessesSystem)
	selectData(p.filter, w.config.ProcessFilter)
	p.view = qt.NewQComboBox2()
	addItem(p.view, "List", settings.ViewList)
	addItem(p.view, "Tree", settings.ViewTree)
	selectData(p.view, w.config.ProcessView)
	p.filter.OnCurrentIndexChanged(func(int) { w.processViewChanged() })
	p.view.OnCurrentIndexChanged(func(int) { w.processViewChanged() })

	p.table = qt.NewQTableWidget3(0, processColumns)
	p.table.VerticalHeader().SetVisible(false)
	p.table.SetSelectionBehavior(qt.QAbstractItemView__SelectRows)
	p.table.SetSelectionMode(qt.QAbstractItemView__SingleSelection)
	p.table.SetEditTriggers(qt.QAbstractItemView__NoEditTriggers)
	p.table.SetShowGrid(false)
	p.table.SetWordWrap(false)
	header := p.table.HorizontalHeader()
	for col, heading := range processHeadings {
		item := qt.NewQTableWidgetItem2(heading)
		item.SetTextAlignment(int(processAlignment(col)))
		if col != colProcessName {
			// A column of numbers is sorted from the largest when it is
			// first chosen, and one of names from A.
			order := qt.NewQVariant4(int(qt.DescendingOrder))
			item.SetData(int(qt.InitialSortOrderRole), order)
			order.Delete()
		}
		p.table.SetHorizontalHeaderItem(col, item)
	}
	// The columns share the width of the table, as fitColumns says, as it
	// grows and shrinks. They are not sized to what they hold, as Qt
	// would measure every row whenever any of them changed. The last has
	// what is left, so that they always fill the width.
	for col := range processColumns {
		header.SetSectionResizeMode2(col, qt.QHeaderView__Fixed)
	}
	header.SetStretchLastSection(true)
	p.table.OnResizeEvent(func(super func(*qt.QResizeEvent), event *qt.QResizeEvent) {
		super(event)
		p.fitColumns()
	})
	// The table is sorted here, not by Qt, so that the numbers are sorted
	// as numbers and the process chosen stays chosen. It is sorted as the
	// settings say, and how it is sorted by clicking a heading is saved.
	header.SetSectionsClickable(true)
	header.SetSortIndicatorShown(true)
	w.applyProcessOrder()
	header.OnSortIndicatorChanged(w.processOrderChanged)
	p.table.OnItemSelectionChanged(w.chooseProcess)
	p.layout.AddWidget2(p.table.QWidget, 1)

	p.signals = qt.NewQComboBox2()
	for _, s := range stats.Signals() {
		p.signals.AddItem(fmt.Sprintf("%s (%d)", s.Name, s.Number))
	}
	p.send = qt.NewQPushButton3("Send Signal")
	p.send.SetEnabled(false)
	p.send.OnClicked(w.sendSignal)
	row := qt.NewQHBoxLayout2()
	row.AddWidget(qt.NewQLabel3("Processes").QWidget)
	row.AddWidget(p.filter.QWidget)
	row.AddSpacing(12)
	row.AddWidget(qt.NewQLabel3("Show as").QWidget)
	row.AddWidget(p.view.QWidget)
	row.AddStretch()
	row.AddWidget(qt.NewQLabel3("Signal").QWidget)
	row.AddWidget(p.signals.QWidget)
	row.AddWidget(p.send.QWidget)
	p.layout.AddLayout(row.QLayout)
}

// processAlignment returns how the column of the table of processes, and its
// heading, are aligned: the PIDs and names to the left, and the use of the
// processor and memory to the right.
func processAlignment(col int) qt.AlignmentFlag {
	if col == colProcessPID || col == colProcessName {
		return qt.AlignLeft | qt.AlignVCenter
	}
	return qt.AlignRight | qt.AlignVCenter
}

// applyProcessOrder sorts the table of processes as the settings say, as it
// was last sorted.
func (w *Window) applyProcessOrder() {
	col := max(slices.Index(processSorts[:], w.config.ProcessSort), 0)
	order := qt.AscendingOrder
	if w.config.ProcessOrder == settings.OrderDescending {
		order = qt.DescendingOrder
	}
	w.processes.table.HorizontalHeader().SetSortIndicator(col, order)
}

// processOrderChanged sorts the table of processes by the column, as its
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
	filter, view := currentData(w.processes.filter), currentData(w.processes.view)
	if w.config.ProcessFilter != filter || w.config.ProcessView != view {
		w.config.ProcessFilter, w.config.ProcessView = filter, view
		w.saveConfig()
	}
	w.showProcesses()
}

// fitColumns makes the PIDs as wide as the largest of them, and gives each
// other column its share of the rest of the width of the header, which is
// that of the table less its scroll bar, when the table is resized or
// filled. The PIDs are measured by how many digits the largest has, not by
// Qt, which would measure every row.
func (p *processes) fitColumns() {
	header := p.table.HorizontalHeader()
	largest := 0
	for _, proc := range p.shown {
		largest = max(largest, proc.PID)
	}
	digits := strings.Repeat("8", len(strconv.Itoa(largest)))
	pid := max(p.table.FontMetrics().HorizontalAdvance(digits),
		header.FontMetrics().HorizontalAdvance(processHeadings[colProcessPID])+sortArrowRoom) + columnPadding
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

// sampleProcesses reads the processes, and lists them with how busy each was
// since they were last read, which is known from a process's second reading.
func (w *Window) sampleProcesses() {
	list, err := w.source.Processes()
	w.processes.report(err)
	if err != nil {
		return
	}
	w.processes.list = w.processes.usage.Update(w.now(), list)
	w.showProcesses()
}

// showProcesses lists the processes the settings choose in the table, as a
// list or a tree, sorted as its header says, with the one chosen still chosen
// if it is still listed.
func (w *Window) showProcesses() {
	p := &w.processes
	p.filling = true
	defer func() { p.filling = false }()
	header := p.table.HorizontalHeader()
	p.shown = filterProcesses(p.list, w.config.ProcessFilter)
	column, descending := header.SortIndicatorSection(), header.SortIndicatorOrder() == qt.DescendingOrder
	if w.config.ProcessView == settings.ViewTree {
		p.shown, p.depths = treeOrder(p.shown, column, descending)
	} else {
		sortProcesses(p.shown, column, descending)
		p.depths = make([]int, len(p.shown))
	}
	rows := p.table.RowCount()
	p.table.SetRowCount(len(p.shown))
	for row := rows; row < len(p.shown); row++ {
		for col := range processColumns {
			item := qt.NewQTableWidgetItem()
			item.SetTextAlignment(int(processAlignment(col)))
			p.table.SetItem(row, col, item)
		}
	}
	u := w.units()
	for row, proc := range p.shown {
		cells := processCells(proc, u)
		cells[colProcessName] = treeIndent(p.depths[row]) + cells[colProcessName]
		for col, text := range cells {
			if item := p.table.Item(row, col); item.Text() != text {
				item.SetText(text)
			}
		}
	}

	chosen := -1
	if p.chosen != nil {
		chosen = slices.IndexFunc(p.shown, func(proc usage.Process) bool {
			return proc.PID == p.chosen.PID && proc.Start == p.chosen.Start
		})
	}
	if chosen < 0 {
		p.table.ClearSelection()
		p.chosen = nil
	} else {
		p.table.SelectRow(chosen)
	}
	p.send.SetEnabled(p.chosen != nil && p.signals.Count() > 0)
	// The rows can have brought a scroll bar, which the header is narrowed
	// by, or a larger PID.
	p.fitColumns()
}

// processCells writes what the table lists of the process: its name, PID,
// how busy it was ("—" while it is not known), and its memory, which is "—"
// for a process the system does not let it be read of, and for a thread of
// the kernel, which has none of its own.
func processCells(p usage.Process, u chart.Units) [processColumns]string {
	cells := [processColumns]string{strconv.Itoa(p.PID), p.Name, "—", "—"}
	if p.Measured {
		cells[colProcessCPU] = chart.FormatPercent(p.Busy)
	}
	if !p.Restricted && !p.Kernel {
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
		case colProcessCPU:
			c = cmp.Compare(known(a.Measured, a.Busy), known(b.Measured, b.Busy))
		case colProcessMemory:
			c = cmp.Compare(known(!a.Restricted && !a.Kernel, float64(a.Memory)), known(!b.Restricted && !b.Kernel, float64(b.Memory)))
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
// settings' Processes, chooses: every one, those of people, or the system's.
func filterProcesses(list []usage.Process, filter string) []usage.Process {
	return slices.DeleteFunc(slices.Clone(list), func(p usage.Process) bool {
		switch filter {
		case settings.ProcessesUser:
			return p.System()
		case settings.ProcessesSystem:
			return !p.System()
		}
		return false
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

// treeIndent returns what is written before the name of a process as deep in
// the tree as the depth: nothing at the top, and below it a space as wide as
// the letter M for each level above its parent's, and a corner.
func treeIndent(depth int) string {
	if depth == 0 {
		return ""
	}
	return strings.Repeat("\u2003", depth-1) + "└ "
}

// chooseProcess keeps which process is chosen in the table, and lets it be
// sent a signal.
func (w *Window) chooseProcess() {
	p := &w.processes
	if p.filling {
		return
	}
	row := -1
	if rows := p.table.SelectionModel().SelectedRows(); len(rows) > 0 {
		row = rows[0].Row()
	}
	if row < 0 || row >= len(p.shown) {
		p.chosen = nil
	} else {
		proc := p.shown[row].Process
		p.chosen = &proc
	}
	p.send.SetEnabled(p.chosen != nil && p.signals.Count() > 0)
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
