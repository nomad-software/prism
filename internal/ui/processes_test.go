package ui

import (
	"errors"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/prism/internal/settings"
	"github.com/nomad-software/prism/internal/stats"
	"github.com/nomad-software/prism/internal/usage"
)

// showAll has every process shown, rather than those of people alone, as
// the processes of most tests are root's.
func showAll(s *settings.Settings) {
	s.ProcessFilter = settings.ProcessesAll
}

// items returns the items of the tree of processes, each before those under
// it, from the top, folded or not, with how deep each is.
func items(tw *testWindow) ([]*qt.QTreeWidgetItem, []int) {
	var all []*qt.QTreeWidgetItem
	var depths []int
	var walk func(item *qt.QTreeWidgetItem, depth int)
	walk = func(item *qt.QTreeWidgetItem, depth int) {
		all = append(all, item)
		depths = append(depths, depth)
		for i := range item.ChildCount() {
			walk(item.Child(i), depth+1)
		}
	}
	tree := tw.processes.tree
	for i := range tree.TopLevelItemCount() {
		walk(tree.TopLevelItem(i), 0)
	}
	return all, depths
}

// column returns the texts of the column of the tree of processes, each
// before those under it, from the top.
func column(tw *testWindow, col int) []string {
	var texts []string
	all, _ := items(tw)
	for _, item := range all {
		texts = append(texts, item.Text(col))
	}
	return texts
}

// depths returns how deep each process of the tree is, from the top.
func depths(tw *testWindow) []int {
	_, d := items(tw)
	return d
}

// chooseRow chooses the process of the row of the tree, counting each before
// those under it, as a click on it would.
func chooseRow(tw *testWindow, row int) {
	all, _ := items(tw)
	tw.processes.tree.SetCurrentItem(all[row])
}

// chosenRow returns the row of the process chosen in the tree, or -1.
func chosenRow(tw *testWindow) int {
	selected := tw.processes.tree.SelectedItems()
	if len(selected) == 0 {
		return -1
	}
	all, _ := items(tw)
	return slices.IndexFunc(all, func(item *qt.QTreeWidgetItem) bool {
		return item.UnsafePointer() == selected[0].UnsafePointer()
	})
}

func TestTabs(t *testing.T) {
	tw := newTestWindow(t, showAll)
	mainthread.Wait(func() {
		if tw.CentralWidget().UnsafePointer() != tw.tabs.QWidget.UnsafePointer() {
			t.Fatal("the tabs are not the window's")
		}
		var titles []string
		for i := range tw.tabs.Count() {
			titles = append(titles, tw.tabs.TabText(i))
		}
		if !slices.Equal(titles, []string{"Resources", "Processes"}) {
			t.Errorf("tabs = %q", titles)
		}
		var headings []string
		for col := range processColumns {
			headings = append(headings, tw.processes.tree.HeaderItem().Text(col))
		}
		if !slices.Equal(headings, []string{"PID", "Name", "CPU", "Memory"}) {
			t.Errorf("headings = %q", headings)
		}
		if tw.processes.signals.CurrentText() != "SIGKILL (9)" {
			t.Errorf("signal = %q, want SIGKILL", tw.processes.signals.CurrentText())
		}
		if tw.processes.send.IsEnabled() {
			t.Error("a signal can be sent with no process chosen")
		}
	})
}

func TestSampleProcesses(t *testing.T) {
	tw := newTestWindow(t, showAll)
	tw.source.set(func(f *fakeSource) {
		f.procs = []stats.Process{
			{PID: 10, Name: "idle", Start: 1, CPU: time.Second, Memory: 1 << 20},
			{PID: 20, Name: "busy", Start: 1, CPU: 4 * time.Second, Memory: 3 << 30},
			{PID: 30, Name: "other user's", Start: 1, Restricted: true},
			{PID: 35, Name: "kworker/0:1", Start: 1, CPU: time.Second, Kernel: true},
		}
	})
	mainthread.Wait(func() {
		tw.sampleProcesses()
		// How busy they are is not known from one reading; they are listed
		// by PID until it is.
		if got := column(tw, colProcessPID); !slices.Equal(got, []string{"10", "20", "30", "35"}) {
			t.Errorf("PIDs = %q", got)
		}
		if got := column(tw, colProcessCPU); !slices.Equal(got, []string{"—", "—", "—", "—"}) {
			t.Errorf("CPU = %q", got)
		}
		if got := column(tw, colProcessMemory); !slices.Equal(got, []string{"1.0 MiB", "3.0 GiB", "—", "—"}) {
			t.Errorf("memory = %q", got)
		}
	})

	tw.advance(2 * time.Second)
	tw.source.set(func(f *fakeSource) {
		f.procs = []stats.Process{
			{PID: 10, Name: "idle", Start: 1, CPU: 1100 * time.Millisecond, Memory: 1 << 20},
			{PID: 20, Name: "busy", Start: 1, CPU: 7 * time.Second, Memory: 3 << 30},
			{PID: 30, Name: "other user's", Start: 1, Restricted: true},
			{PID: 40, Name: "new", Start: 1, CPU: time.Second},
		}
	})
	mainthread.Wait(func() {
		tw.sampleProcesses()
		// The busiest are first, and those not known last.
		if got := column(tw, colProcessName); !slices.Equal(got, []string{"busy", "idle", "other user's", "new"}) {
			t.Errorf("names = %q", got)
		}
		if got := column(tw, colProcessCPU); !slices.Equal(got, []string{"150.0%", "5.0%", "—", "—"}) {
			t.Errorf("CPU = %q", got)
		}
		// The PIDs and names, and their headings, are to the left, and the
		// rest to the right.
		for col, want := range map[int]qt.AlignmentFlag{
			colProcessPID: qt.AlignLeft, colProcessName: qt.AlignLeft,
			colProcessCPU: qt.AlignRight, colProcessMemory: qt.AlignRight,
		} {
			if a := tw.processes.tree.TopLevelItem(0).TextAlignment(col); a&int(want) == 0 {
				t.Errorf("column %d is aligned %#x", col, a)
			}
			if a := tw.processes.tree.HeaderItem().TextAlignment(col); a&int(want) == 0 {
				t.Errorf("heading %d is aligned %#x", col, a)
			}
		}
	})
}

func TestProcessesError(t *testing.T) {
	tw := newTestWindow(t, showAll)
	tw.source.set(func(f *fakeSource) { f.err = errors.New("no /proc") })
	mainthread.Wait(func() {
		tw.sampleProcesses()
		if !tw.processes.errors.IsVisibleTo(tw.processes.QWidget) || !strings.Contains(tw.processes.errors.Text(), "no /proc") {
			t.Errorf("the error is not said: %q", tw.processes.errors.Text())
		}
	})
}

func TestSortProcessesByHeader(t *testing.T) {
	tw := newTestWindow(t, showAll)
	tw.source.set(func(f *fakeSource) {
		f.procs = []stats.Process{
			{PID: 3, Name: "bravo", Memory: 100, CPU: 3 * time.Second},
			{PID: 1, Name: "Charlie", Memory: 300, CPU: 2 * time.Second},
			{PID: 2, Name: "alpha", Memory: 200, CPU: time.Second},
		}
	})
	mainthread.Wait(func() {
		tw.sampleProcesses()
		header := tw.processes.tree.Header()
		for _, c := range []struct {
			col   int
			order qt.SortOrder
			want  []string
		}{
			{colProcessName, qt.AscendingOrder, []string{"alpha", "bravo", "Charlie"}},
			{colProcessName, qt.DescendingOrder, []string{"Charlie", "bravo", "alpha"}},
			{colProcessPID, qt.AscendingOrder, []string{"Charlie", "alpha", "bravo"}},
			{colProcessPID, qt.DescendingOrder, []string{"bravo", "alpha", "Charlie"}},
			{colProcessMemory, qt.DescendingOrder, []string{"Charlie", "alpha", "bravo"}},
		} {
			header.SetSortIndicator(c.col, c.order)
			if got := column(tw, colProcessName); !slices.Equal(got, c.want) {
				t.Errorf("sorted by %d, %v: %q, want %q", c.col, c.order, got, c.want)
			}
		}
	})
}

func TestSortProcesses(t *testing.T) {
	list := []usage.Process{
		{Process: stats.Process{PID: 4}},
		{Process: stats.Process{PID: 2}, Busy: 0, Measured: true},
		{Process: stats.Process{PID: 3}, Busy: 50, Measured: true},
		{Process: stats.Process{PID: 1}, Busy: 50, Measured: true},
	}
	pids := func() []int {
		var pids []int
		for _, p := range list {
			pids = append(pids, p.PID)
		}
		return pids
	}
	// Those of the same are by PID, and one not known is less than one of 0.
	sortProcesses(list, colProcessCPU, true)
	if got := pids(); !slices.Equal(got, []int{1, 3, 2, 4}) {
		t.Errorf("descending = %v", got)
	}
	sortProcesses(list, colProcessCPU, false)
	if got := pids(); !slices.Equal(got, []int{4, 2, 1, 3}) {
		t.Errorf("ascending = %v", got)
	}
}

func TestChosenProcessKept(t *testing.T) {
	tw := newTestWindow(t, showAll)
	tw.source.set(func(f *fakeSource) {
		f.procs = []stats.Process{{PID: 1, Name: "a"}, {PID: 2, Name: "b"}, {PID: 3, Name: "c"}}
	})
	mainthread.Wait(func() {
		tw.sampleProcesses()
		chooseRow(tw, 2)
		if tw.processes.chosen == nil || tw.processes.chosen.PID != 3 || !tw.processes.send.IsEnabled() {
			t.Fatalf("chosen = %+v", tw.processes.chosen)
		}
	})
	// It is still chosen when it moves, and when rows go from under it.
	tw.advance(time.Second)
	tw.source.set(func(f *fakeSource) {
		f.procs = []stats.Process{{PID: 3, Name: "c"}}
	})
	mainthread.Wait(func() {
		tw.sampleProcesses()
		if tw.processes.chosen == nil || tw.processes.chosen.PID != 3 || chosenRow(tw) != 0 {
			t.Errorf("chosen = %+v, row %d", tw.processes.chosen, chosenRow(tw))
		}
	})
	// A process given its PID once it has ended is another.
	tw.advance(time.Second)
	tw.source.set(func(f *fakeSource) {
		f.procs = []stats.Process{{PID: 3, Name: "c", Start: 99}}
	})
	mainthread.Wait(func() {
		tw.sampleProcesses()
		if tw.processes.chosen != nil || tw.processes.send.IsEnabled() || len(tw.processes.tree.SelectedItems()) > 0 {
			t.Errorf("chosen = %+v after it ended", tw.processes.chosen)
		}
	})
}

func TestSendSignal(t *testing.T) {
	tw := newTestWindow(t, showAll)
	tw.source.set(func(f *fakeSource) {
		f.procs = []stats.Process{{PID: 42, Name: "victim"}}
	})
	mainthread.Wait(func() {
		tw.sampleProcesses()
		chooseRow(tw, 0)
		tw.processes.send.Click()
		if msg := tw.StatusBar().CurrentMessage(); msg != "Sent SIGKILL to victim (42)" {
			t.Errorf("message = %q", msg)
		}

		tw.processes.signals.SetCurrentText("SIGTERM (15)")
		tw.source.set(func(f *fakeSource) { f.signalErr = syscall.EPERM })
		tw.processes.send.Click()
		if msg := tw.StatusBar().CurrentMessage(); msg != "SIGTERM could not be sent to victim (42): operation not permitted" {
			t.Errorf("message = %q", msg)
		}
	})
	tw.source.Lock()
	defer tw.source.Unlock()
	if want := []string{"42 9", "42 15"}; !slices.Equal(tw.source.signalled, want) {
		t.Errorf("signalled %q, want %q", tw.source.signalled, want)
	}
}

// TestSortProcessesByClick clicks the headings of the table of processes, as
// the mouse would.
func TestSortProcessesByClick(t *testing.T) {
	tw := newTestWindow(t, showAll)
	tw.source.set(func(f *fakeSource) {
		f.procs = []stats.Process{{PID: 3, Name: "b", Memory: 1}, {PID: 1, Name: "c", Memory: 3}, {PID: 2, Name: "a", Memory: 2}}
	})
	mainthread.Wait(func() {
		tw.tabs.SetCurrentIndex(1)
		tw.sampleProcesses()
		header := tw.processes.tree.Header()
		click := func(col int) {
			x := float64(header.SectionViewportPosition(col) + header.SectionSize(col)/2)
			pos := qt.NewQPointF3(x, float64(header.Height()/2))
			defer pos.Delete()
			for _, kind := range []qt.QEvent__Type{qt.QEvent__MouseButtonPress, qt.QEvent__MouseButtonRelease} {
				e := qt.NewQMouseEvent(kind, pos, qt.LeftButton, qt.LeftButton, qt.NoModifier)
				qt.QCoreApplication_SendEvent(header.Viewport().QObject, e.QEvent)
				e.Delete()
			}
		}
		for _, c := range []struct {
			col  int
			want []string
		}{
			// Names are from A when first clicked, and the other way when
			// clicked again; numbers are from the largest.
			{colProcessName, []string{"a", "b", "c"}},
			{colProcessName, []string{"c", "b", "a"}},
			{colProcessMemory, []string{"c", "a", "b"}},
			{colProcessPID, []string{"b", "a", "c"}},
		} {
			click(c.col)
			if got := column(tw, colProcessName); !slices.Equal(got, c.want) {
				t.Errorf("clicked %d: %q, want %q", c.col, got, c.want)
			}
		}
	})
}

// TestProcessColumnWidths checks that the PIDs are first, as wide as the
// largest of them, and that the other columns share the rest of the width of
// the table as it changes: the name half, and the others a quarter each.
func TestProcessColumnWidths(t *testing.T) {
	tw := newTestWindow(t, showAll)
	tw.source.set(func(f *fakeSource) {
		f.procs = []stats.Process{{PID: 1, Name: "init"}, {PID: 4194304, Name: "late"}}
	})
	mainthread.Wait(func() {
		tw.tabs.SetCurrentIndex(1)
		tw.sampleProcesses()
		header := tw.processes.tree.Header()
		if header.LogicalIndexAt(1) != colProcessPID || tw.processes.tree.HeaderItem().Text(0) != "PID" {
			t.Fatal("the PIDs are not first")
		}
		fm := tw.processes.tree.FontMetrics()
		pid := header.SectionSize(colProcessPID)
		if pid < fm.HorizontalAdvance("4194304") || pid > fm.HorizontalAdvance("4194304")+2*columnPadding {
			t.Errorf("the PIDs are %d wide, not sized to 4194304", pid)
		}
		for _, width := range []int{900, 1400, 600} {
			tw.Resize(width, 820)
			qt.QCoreApplication_ProcessEvents()
			quarter := (header.Width() - pid) / 4
			want := [processColumns]int{pid, 2 * quarter, quarter, quarter}
			sum := 0
			for col := range processColumns {
				got := header.SectionSize(col)
				sum += got
				if got < want[col]-2 || got > want[col]+5 {
					t.Errorf("window %d: column %d is %d wide, want about %d", width, col, got, want[col])
				}
			}
			if sum != header.Width() {
				t.Errorf("window %d: columns are %d wide in all, want %d", width, sum, header.Width())
			}
		}
	})
	// The PIDs narrow when the largest has gone.
	tw.source.set(func(f *fakeSource) { f.procs = []stats.Process{{PID: 1, Name: "init"}} })
	mainthread.Wait(func() {
		before := tw.processes.tree.Header().SectionSize(colProcessPID)
		tw.sampleProcesses()
		if after := tw.processes.tree.Header().SectionSize(colProcessPID); after >= before {
			t.Errorf("the PIDs are %d wide, were %d, with only PID 1", after, before)
		}
	})
}

// TestProcessOrderSetting checks that the table is sorted as the settings
// say from the start, and that sorting it by clicking a heading is saved.
func TestProcessOrderSetting(t *testing.T) {
	tw := newTestWindow(t, func(s *settings.Settings) {
		s.ProcessSort, s.ProcessOrder = settings.SortName, settings.OrderDescending
		showAll(s)
	})
	tw.source.set(func(f *fakeSource) {
		f.procs = []stats.Process{{PID: 3, Name: "b", Memory: 1}, {PID: 1, Name: "c", Memory: 3}, {PID: 2, Name: "a", Memory: 2}}
	})
	mainthread.Wait(func() {
		tw.sampleProcesses()
		header := tw.processes.tree.Header()
		if header.SortIndicatorSection() != colProcessName || header.SortIndicatorOrder() != qt.DescendingOrder {
			t.Error("the table is not sorted as the settings say")
		}
		if got := column(tw, colProcessName); !slices.Equal(got, []string{"c", "b", "a"}) {
			t.Errorf("names = %q", got)
		}

		header.SetSortIndicator(colProcessPID, qt.AscendingOrder)
		if s := settings.Load(tw.path); s.ProcessSort != settings.SortPID || s.ProcessOrder != settings.OrderAscending {
			t.Errorf("saved order = %q, %q", s.ProcessSort, s.ProcessOrder)
		}

		if got := column(tw, colProcessName); !slices.Equal(got, []string{"c", "a", "b"}) {
			t.Errorf("names by PID = %q", got)
		}
	})
}

func TestTabShortcuts(t *testing.T) {
	tw := newTestWindow(t, showAll)
	mainthread.Wait(func() {
		if len(tw.tabKeys) != tabCount {
			t.Fatalf("%d shortcuts, want %d", len(tw.tabKeys), tabCount)
		}
		for i, want := range []string{"Ctrl+1", "Ctrl+2"} {
			if got := tw.tabKeys[i].Key().ToString(); got != want {
				t.Errorf("shortcut %d = %q, want %q", i, got, want)
			}
		}
		tw.tabKeys[tabProcesses].Activated()
		if tw.tabs.CurrentIndex() != tabProcesses {
			t.Error("Ctrl+2 does not show the processes")
		}
		tw.tabKeys[tabResources].Activated()
		if tw.tabs.CurrentIndex() != tabResources {
			t.Error("Ctrl+1 does not show the resources")
		}
	})
}

// iconLightness returns the lightness of the most opaque pixel of the icon of
// the tab, from 0 to 255, or -1 if it has none.
func iconLightness(tw *testWindow, tab int) int {
	pixmap := tw.tabs.TabIcon(tab).Pixmap2(tabIconSize, tabIconSize)
	img := pixmap.ToImage()
	best, lightness := 0, -1
	for y := range img.Height() {
		for x := range img.Width() {
			c := img.PixelColor(x, y)
			if c.Alpha() > best {
				best, lightness = c.Alpha(), c.Lightness()
			}
		}
	}
	return lightness
}

func TestTabIcons(t *testing.T) {
	tw := newTestWindow(t, func(s *settings.Settings) { s.Theme = settings.ThemeDark })
	mainthread.Wait(func() {
		for tab := range tabCount {
			if tw.tabs.TabIcon(tab).IsNull() {
				t.Fatalf("tab %d has no icon", tab)
			}
			// The icons are in the colour of text: light on dark.
			if l := iconLightness(tw, tab); l < 160 {
				t.Errorf("dark theme: tab %d icon lightness %d, want light", tab, l)
			}
		}
		s := tw.config
		s.Theme = settings.ThemeLight
		tw.applySettings(s)
		qt.QCoreApplication_ProcessEvents()
		for tab := range tabCount {
			if l := iconLightness(tw, tab); l < 0 || l > 100 {
				t.Errorf("light theme: tab %d icon lightness %d, want dark", tab, l)
			}
		}
	})
}

// treeProcesses are processes as they are started: init starts a service and
// a login; the login, of a person, starts a shell, which starts an editor;
// and a kernel thread has no parent.
var treeProcesses = []stats.Process{
	{PID: 1, Name: "init", Memory: 10},
	{PID: 2, Name: "kthreadd", Kernel: true},
	{PID: 300, PPID: 1, Name: "sshd", Memory: 30},
	{PID: 400, PPID: 1, Name: "login", UID: 1000, Memory: 20},
	{PID: 500, PPID: 400, Name: "bash", UID: 1000, Memory: 50},
	{PID: 600, PPID: 500, Name: "vim", UID: 1000, Memory: 40},
}

func TestProcessFilterAndView(t *testing.T) {
	tw := newTestWindow(t, func(s *settings.Settings) { s.ProcessSort, s.ProcessOrder = settings.SortPID, settings.OrderAscending })
	tw.source.set(func(f *fakeSource) { f.procs = treeProcesses })
	mainthread.Wait(func() {
		p := &tw.processes
		tw.sampleProcesses()
		// Those of people are shown as a list at first.
		if currentData(p.filter) != settings.ProcessesUser || currentData(p.view) != settings.ViewList {
			t.Error("the processes of people are not shown as a list at first")
		}
		if got := column(tw, colProcessName); !slices.Equal(got, []string{"login", "bash", "vim"}) {
			t.Errorf("user = %q", got)
		}

		selectData(p.filter, settings.ProcessesAll)
		if got := column(tw, colProcessName); !slices.Equal(got, []string{"init", "kthreadd", "sshd", "login", "bash", "vim"}) {
			t.Errorf("all = %q", got)
		}
		selectData(p.filter, settings.ProcessesSystem)
		if got := column(tw, colProcessName); !slices.Equal(got, []string{"init", "kthreadd", "sshd"}) {
			t.Errorf("system = %q", got)
		}

		// A tree has each process under the one that started it, and those
		// of the same parent in the order chosen.
		selectData(p.filter, settings.ProcessesAll)
		selectData(p.view, settings.ViewTree)
		tw.processes.tree.Header().SetSortIndicator(colProcessMemory, qt.DescendingOrder)
		want := []string{"init", "sshd", "login", "bash", "vim", "kthreadd"}
		if got := column(tw, colProcessName); !slices.Equal(got, want) {
			t.Errorf("tree = %q, want %q", got, want)
		}
		if got := depths(tw); !slices.Equal(got, []int{0, 1, 1, 2, 3, 0}) {
			t.Errorf("tree depths = %v", got)
		}
		if !p.tree.RootIsDecorated() {
			t.Error("the tree has no branches to fold")
		}
		// One whose parent is not shown is at the top.
		selectData(p.filter, settings.ProcessesUser)
		if got := column(tw, colProcessName); !slices.Equal(got, []string{"login", "bash", "vim"}) || !slices.Equal(depths(tw), []int{0, 1, 2}) {
			t.Errorf("user tree = %q, %v", got, depths(tw))
		}
		// The process chosen is the one in its row.
		chooseRow(tw, 2)
		if p.chosen == nil || p.chosen.PID != 600 {
			t.Errorf("chosen = %+v, want vim", p.chosen)
		}
	})
	if s := settings.Load(tw.path); s.ProcessFilter != settings.ProcessesUser || s.ProcessView != settings.ViewTree {
		t.Errorf("saved %q, %q", s.ProcessFilter, s.ProcessView)
	}

	// They are as they were saved when the window is made again.
	again := newTestWindow(t, func(s *settings.Settings) { *s = settings.Load(tw.path) })
	mainthread.Wait(func() {
		if currentData(again.processes.filter) != settings.ProcessesUser || currentData(again.processes.view) != settings.ViewTree {
			t.Error("the choices are not as they were saved")
		}
	})
}

// TestProcessChoicesBelowTable checks that the choices of which processes
// are shown, and how, are below the table, at its left.
func TestProcessChoicesBelowTable(t *testing.T) {
	tw := newTestWindow(t, showAll)
	mainthread.Wait(func() {
		tw.tabs.SetCurrentIndex(tabProcesses)
		qt.QCoreApplication_ProcessEvents()
		p := &tw.processes
		filter, view := p.filter.Geometry(), p.view.Geometry()
		table := p.tree.Geometry()
		var items []string
		for i := range p.filter.Count() {
			items = append(items, p.filter.ItemText(i))
		}
		if !slices.Equal(items, []string{"All", "User", "System"}) {
			t.Errorf("filter = %q", items)
		}
		// Each is labelled, the label just before it.
		for _, c := range []struct {
			text string
			box  *qt.QComboBox
		}{{"Processes", p.filter}, {"Show as", p.view}} {
			found := false
			for _, label := range labels(p.QObject) {
				if label.Text() == c.text {
					g := label.Geometry()
					found = g.Right() < c.box.Geometry().Left() && c.box.Geometry().Left()-g.Right() < 20 && g.Top() > table.Bottom()
				}
			}
			if !found {
				t.Errorf("there is no label %q just before its dropdown", c.text)
			}
		}
		if filter.Top() <= table.Bottom() || view.Top() <= table.Bottom() {
			t.Error("the choices are not below the table")
		}
		if filter.Left() > table.Left()+table.Width()/4 || view.Left() <= filter.Right() || view.Right() >= p.signals.Geometry().Left() {
			t.Errorf("the choices are not at the left: filter %d-%d, view %d-%d, table from %d",
				filter.Left(), filter.Right(), view.Left(), view.Right(), table.Left())
		}
	})
}

// TestProcessSearch checks that what is written in the search above the
// table narrows the processes to those whose names or PIDs hold it, in
// either view.
func TestProcessSearch(t *testing.T) {
	tw := newTestWindow(t, func(s *settings.Settings) {
		showAll(s)
		s.ProcessSort, s.ProcessOrder = settings.SortPID, settings.OrderAscending
	})
	tw.source.set(func(f *fakeSource) { f.procs = treeProcesses })
	mainthread.Wait(func() {
		p := &tw.processes
		tw.tabs.SetCurrentIndex(tabProcesses)
		qt.QCoreApplication_ProcessEvents()
		if p.search.Geometry().Bottom() >= p.tree.Geometry().Top() {
			t.Error("the search is not above the table")
		}
		tw.sampleProcesses()
		for _, c := range []struct {
			text string
			want []string
		}{
			{"SH", []string{"sshd", "bash"}},
			{"  vim ", []string{"vim"}},
			{"00", []string{"sshd", "login", "bash", "vim"}},
			{"nothing", nil},
			{"", []string{"init", "kthreadd", "sshd", "login", "bash", "vim"}},
		} {
			p.search.SetText(c.text)
			if got := column(tw, colProcessName); !slices.Equal(got, c.want) {
				t.Errorf("search %q = %q, want %q", c.text, got, c.want)
			}
		}
		// In a tree, one whose parent is not found is at the top.
		selectData(p.view, settings.ViewTree)
		p.search.SetText("o")
		if got := column(tw, colProcessName); !slices.Equal(got, []string{"login"}) {
			t.Errorf("tree search = %q", got)
		}
		p.search.SetText("i")
		if got := column(tw, colProcessName); !slices.Equal(got, []string{"init", "login", "vim"}) || !slices.Equal(depths(tw), []int{0, 1, 0}) {
			t.Errorf("tree search = %q, %v", got, depths(tw))
		}
		// It stays as it is when the processes are read again.
		tw.sampleProcesses()
		if got := column(tw, colProcessName); len(got) != 3 {
			t.Errorf("after reading again = %q", got)
		}
	})
}

func TestTreeOrderRing(t *testing.T) {
	// Processes that are each other's parents, as cannot be, are all listed.
	list := []usage.Process{{Process: stats.Process{PID: 1, PPID: 2}}, {Process: stats.Process{PID: 2, PPID: 1}}}
	got, depths := treeOrder(list, colProcessPID, false)
	if len(got) != 2 || len(depths) != 2 {
		t.Errorf("treeOrder of a ring = %+v, %v", got, depths)
	}
}

// TestTabPadding checks that the tabs have room beyond what the style gives
// them.
func TestTabPadding(t *testing.T) {
	tw := newTestWindow(t, nil)
	mainthread.Wait(func() {
		bar := tw.tabs.TabBar()
		// A tab widget as the window's, but with the style's own tabs.
		plain := qt.NewQTabWidget2()
		defer plain.Delete()
		plain.SetDocumentMode(true)
		plain.SetFont(tw.tabs.Font())
		for i := range tabCount {
			plain.AddTab2(qt.NewQWidget2(), tw.tabs.TabIcon(i), tw.tabs.TabText(i))
		}
		plain.Resize(tw.tabs.Width(), tw.tabs.Height())
		for i := range tabCount {
			got, natural := bar.TabRect(i), plain.TabBar().TabRect(i)
			if got.Width() < natural.Width()+tabPadX || got.Height() < natural.Height()+tabPadY {
				t.Errorf("tab %d is %dx%d, want at least %dx%d", i, got.Width(), got.Height(), natural.Width()+tabPadX, natural.Height()+tabPadY)
			}
		}
	})
}

// TestProcessFolding checks that the branches of the tree fold and open, and
// stay as they were left as the processes are read again and sorted, and
// that a list has no branches.
func TestProcessFolding(t *testing.T) {
	tw := newTestWindow(t, func(s *settings.Settings) {
		showAll(s)
		s.ProcessView = settings.ViewTree
		s.ProcessSort, s.ProcessOrder = settings.SortPID, settings.OrderAscending
	})
	tw.source.set(func(f *fakeSource) { f.procs = treeProcesses })
	mainthread.Wait(func() {
		p := &tw.processes
		tw.sampleProcesses()
		find := func(name string) *qt.QTreeWidgetItem {
			all, _ := items(tw)
			for _, item := range all {
				if item.Text(colProcessName) == name {
					return item
				}
			}
			t.Fatalf("no %s", name)
			return nil
		}
		// Every branch is open at first.
		for _, name := range []string{"init", "login", "bash"} {
			if !find(name).IsExpanded() {
				t.Errorf("%s is folded at first", name)
			}
		}

		// The user folds login's branch, which stays folded as the
		// processes are read again and sorted.
		find("login").SetExpanded(false)
		tw.advance(time.Second)
		tw.sampleProcesses()
		p.tree.Header().SetSortIndicator(colProcessName, qt.DescendingOrder)
		if find("login").IsExpanded() || !find("init").IsExpanded() || !find("bash").IsExpanded() {
			t.Error("the branches are not as they were left")
		}
		// The process chosen can be under a folded branch, and stays chosen.
		p.tree.SetCurrentItem(find("vim"))
		tw.sampleProcesses()
		if p.chosen == nil || p.chosen.PID != 600 {
			t.Errorf("chosen = %+v, want vim", p.chosen)
		}
		// Opened again, it stays open.
		find("login").SetExpanded(true)
		tw.sampleProcesses()
		if !find("login").IsExpanded() {
			t.Error("login is folded again")
		}

		// A process that ends is forgotten, so one given its PID is open.
		find("login").SetExpanded(false)
		tw.source.set(func(f *fakeSource) {
			f.procs = slices.DeleteFunc(slices.Clone(treeProcesses), func(p stats.Process) bool { return p.PID == 400 })
		})
		tw.sampleProcesses()
		if len(p.folded) != 0 {
			t.Errorf("folded = %v after login ended", p.folded)
		}

		selectData(p.view, settings.ViewList)
		if p.tree.RootIsDecorated() || !slices.Equal(depths(tw), make([]int, len(depths(tw)))) {
			t.Error("the list has branches")
		}
	})
}

// TestProcessesUnchosen checks that nothing is sent a signal when no process
// is chosen, and that what is folded is kept only of the tree's own items.
func TestProcessesUnchosen(t *testing.T) {
	tw := newTestWindow(t, showAll)
	tw.source.set(func(f *fakeSource) { f.procs = treeProcesses })
	mainthread.Wait(func() {
		p := &tw.processes
		tw.sampleProcesses()
		tw.sendSignal()
		if msg := tw.StatusBar().CurrentMessage(); msg != "" {
			t.Errorf("message = %q with no process chosen", msg)
		}
		// A signal not in the list, as none is chosen, is not sent.
		chooseRow(tw, 0)
		p.signals.SetCurrentIndex(-1)
		tw.sendSignal()

		stranger := qt.NewQTreeWidgetItem()
		defer stranger.Delete()
		p.fold(stranger, true)
		if len(p.folded) != 0 {
			t.Errorf("folded = %v after an item not in the tree", p.folded)
		}
	})
	tw.source.Lock()
	defer tw.source.Unlock()
	if len(tw.source.signalled) != 0 {
		t.Errorf("signalled %q", tw.source.signalled)
	}
}
