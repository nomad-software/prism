// Package settings stores the settings of the program, which are kept
// between runs in a JSON file where ApeMail keeps its own: in a folder for
// the program in the place the system has for the configuration of programs.
// It has no dependency on the UI.
package settings

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	defaultWidth  = 900
	defaultHeight = 820
	minSize       = 200
)

// The intervals the statistics can be read at, in seconds, to a tenth of a
// second.
const (
	MinInterval = 0.1
	MaxInterval = 60 * 60
)

// DefaultIntervals are the intervals the statistics are read at unless set:
// the processors and memory each second, the network adapters and the
// processes every two.
var DefaultIntervals = Intervals{CPU: 1, Memory: 1, Network: 2, Processes: 2}

// The themes that can be chosen, besides the system's, which is "".
const (
	ThemeLight = "light"
	ThemeDark  = "dark"
)

// The ways the use of the processors can be shown: a line for each, or one
// for all of them together.
const (
	CPUCores = "cores"
	CPUTotal = "total"
)

// The units sizes can be written in: of 1024 (KiB, MiB) or of 1000 (kB,
// MB).
const (
	SizeBinary  = "binary"
	SizeDecimal = "decimal"
)

// The units network rates can be written in: bytes or bits a second.
const (
	RateBytes = "bytes"
	RateBits  = "bits"
)

// The columns the processes can be sorted by.
const (
	SortPID    = "pid"
	SortName   = "name"
	SortCPU    = "cpu"
	SortCores  = "cores"
	SortMemory = "memory"
)

// The ways the processes can be sorted: from the least, or A, or from the
// largest, or Z.
const (
	OrderAscending  = "ascending"
	OrderDescending = "descending"
)

// The processes that can be shown: every one, those of people, or the
// system's.
const (
	ProcessesAll    = "all"
	ProcessesUser   = "user"
	ProcessesSystem = "system"
)

// The ways the processes can be shown: as a list, or as a tree of the
// processes each started.
const (
	ViewList = "list"
	ViewTree = "tree"
)

// Settings is the state that is saved to disk.
type Settings struct {
	// Width and Height are the size of the window when it is not maximised.
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	Maximized bool `json:"maximized"`

	// Theme is how the program looks: ThemeLight, ThemeDark, or, where it
	// is empty, as the desktop's theme says.
	Theme string `json:"theme,omitempty"`

	// Intervals are how often each statistic is read.
	Intervals Intervals `json:"intervals"`

	// CPUMode is CPUCores to show a line for each processor, or CPUTotal to
	// show one for all of them.
	CPUMode string `json:"cpuMode"`

	// SizeUnits is SizeBinary or SizeDecimal, for sizes and rates, and
	// RateUnits is RateBytes or RateBits, for network rates.
	SizeUnits string `json:"sizeUnits"`
	RateUnits string `json:"rateUnits"`

	// ProcessSort is the column the processes are sorted by, SortPID,
	// SortName, SortCPU, SortCores or SortMemory, and ProcessOrder is
	// OrderAscending or OrderDescending, as they were last sorted in the
	// window.
	ProcessSort  string `json:"processSort"`
	ProcessOrder string `json:"processOrder"`
	// ProcessFilter is which processes are shown, ProcessesAll,
	// ProcessesUser or ProcessesSystem, and ProcessView is ViewList or
	// ViewTree, as they were last chosen in the window.
	ProcessFilter string `json:"processFilter"`
	ProcessView   string `json:"processView"`

	Colours Colours `json:"colours"`

	// HiddenAdapters are the names of the network adapters not shown in
	// the network graphs and legend, in order.
	HiddenAdapters []string `json:"hiddenAdapters,omitempty"`

	// AdapterAliases are the names the network adapters are shown by in the
	// window, by the names of the adapters. An adapter without one is shown
	// by its own name.
	AdapterAliases map[string]string `json:"adapterAliases,omitempty"`
}

// Shown says whether the network adapter with the name is shown.
func (s Settings) Shown(adapter string) bool {
	return !slices.Contains(s.HiddenAdapters, adapter)
}

// AdapterLabel returns the name the network adapter with the name is shown
// by: its alias, or its own name if it has none.
func (s Settings) AdapterLabel(adapter string) string {
	if alias := s.AdapterAliases[adapter]; alias != "" {
		return alias
	}
	return adapter
}

// Intervals are how often each statistic is read, in seconds to a tenth of
// a second, from MinInterval to MaxInterval.
type Intervals struct {
	CPU       float64 `json:"cpu"`
	Memory    float64 `json:"memory"`
	Network   float64 `json:"network"`
	Processes float64 `json:"processes"`
}

// Duration returns an interval as a duration, to the millisecond.
func Duration(seconds float64) time.Duration {
	return time.Duration(math.Round(seconds*1000)) * time.Millisecond
}

// Defaults returns the settings the program starts with.
func Defaults() Settings {
	return Settings{
		Width:     defaultWidth,
		Height:    defaultHeight,
		Intervals: DefaultIntervals,
		CPUMode:   CPUCores,
		SizeUnits: SizeDecimal,
		RateUnits: RateBits,
		// The busiest processes are first.
		ProcessSort:   SortCPU,
		ProcessOrder:  OrderDescending,
		ProcessFilter: ProcessesUser,
		ProcessView:   ViewList,
		Colours:       DefaultColours(),
	}
}

// appName is the name of the folder the settings are kept in.
const appName = "prism"

// fileName is the name of the settings file.
const fileName = "settings.json"

// Path returns where the settings file is kept: in a folder for the program
// in the place the system has for the configuration of programs, which is
// ~/.config on Linux (or $XDG_CONFIG_HOME) and ~/Library/Application Support
// on macOS.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appName, fileName), nil
}

// Prepared returns where the settings file is kept, as Path does, with the
// folder it is kept in made.
func Prepared() (string, error) {
	path, err := Path()
	if err != nil {
		return "", err
	}
	if err := Prepare(path); err != nil {
		return "", err
	}
	return path, nil
}

// Prepare makes the folder the settings file at path is kept in.
func Prepare(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o700)
}

// Load reads the settings from the JSON file at path. Defaults are used if
// the file is missing or unreadable, and for any value that is unusable.
func Load(path string) Settings {
	s := Defaults()
	if data, err := os.ReadFile(path); err == nil {
		if json.Unmarshal(data, &s) != nil {
			s = Defaults()
		}
	}
	s.normalise()
	return s
}

// normalise puts a default in place of each value that is unusable.
func (s *Settings) normalise() {
	d := Defaults()
	if s.Width < minSize || s.Height < minSize {
		s.Width, s.Height = d.Width, d.Height
	}
	if s.Theme != ThemeLight && s.Theme != ThemeDark {
		s.Theme = ""
	}
	if s.CPUMode != CPUCores && s.CPUMode != CPUTotal {
		s.CPUMode = d.CPUMode
	}
	if s.SizeUnits != SizeBinary && s.SizeUnits != SizeDecimal {
		s.SizeUnits = d.SizeUnits
	}
	if s.RateUnits != RateBytes && s.RateUnits != RateBits {
		s.RateUnits = d.RateUnits
	}
	if !slices.Contains([]string{SortPID, SortName, SortCPU, SortCores, SortMemory}, s.ProcessSort) {
		s.ProcessSort = d.ProcessSort
	}
	if s.ProcessOrder != OrderAscending && s.ProcessOrder != OrderDescending {
		s.ProcessOrder = d.ProcessOrder
	}
	if !slices.Contains([]string{ProcessesAll, ProcessesUser, ProcessesSystem}, s.ProcessFilter) {
		s.ProcessFilter = d.ProcessFilter
	}
	if s.ProcessView != ViewList && s.ProcessView != ViewTree {
		s.ProcessView = d.ProcessView
	}
	for _, i := range []struct {
		value    *float64
		fallback float64
	}{
		{&s.Intervals.CPU, d.Intervals.CPU},
		{&s.Intervals.Memory, d.Intervals.Memory},
		{&s.Intervals.Network, d.Intervals.Network},
		{&s.Intervals.Processes, d.Intervals.Processes},
	} {
		// An interval is kept to a tenth of a second, as it is set; one out
		// of range, or not a number, is the default.
		*i.value = math.Round(*i.value*10) / 10
		if !(*i.value >= MinInterval && *i.value <= MaxInterval) {
			*i.value = i.fallback
		}
	}
	s.Colours.normalise()
	slices.Sort(s.HiddenAdapters)
	s.HiddenAdapters = slices.Compact(s.HiddenAdapters)
	// An alias of nothing but spaces is none.
	for name, alias := range s.AdapterAliases {
		if alias = strings.TrimSpace(alias); alias == "" {
			delete(s.AdapterAliases, name)
		} else {
			s.AdapterAliases[name] = alias
		}
	}
	if len(s.AdapterAliases) == 0 {
		s.AdapterAliases = nil
	}
}

// Save writes the settings to the JSON file at path, replacing it
// atomically, so that a failure leaves the old file whole.
func (s Settings) Save(path string) error {
	// The settings are only strings, numbers and maps of strings, which
	// JSON always writes.
	data, _ := json.MarshalIndent(s, "", "  ")
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
