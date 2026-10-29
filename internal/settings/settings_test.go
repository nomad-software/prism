package settings

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/somewhere")
	t.Setenv("HOME", "/home/someone")
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, filepath.Join("prism", "settings.json")) {
		t.Errorf("Path = %q", path)
	}
}

func TestPrepare(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "settings.json")
	if err := Prepare(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Errorf("folder is %v", info.Mode())
	}
}

func TestDuration(t *testing.T) {
	if d := Duration(90); d != 90*time.Second {
		t.Errorf("Duration(90) = %v", d)
	}
	if d := Duration(0.3); d != 300*time.Millisecond {
		t.Errorf("Duration(0.3) = %v", d)
	}
}

func TestLoadMissing(t *testing.T) {
	s := Load(filepath.Join(t.TempDir(), "settings.json"))
	if !reflect.DeepEqual(s, Defaults()) {
		t.Errorf("Load of a missing file = %+v, want the defaults", s)
	}
}

func TestLoadUnreadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if s := Load(path); !reflect.DeepEqual(s, Defaults()) {
		t.Errorf("Load of a broken file = %+v, want the defaults", s)
	}
}

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := Defaults()
	s.Width, s.Height, s.Maximized = 1000, 700, true
	s.Theme = ThemeDark
	s.Intervals = Intervals{CPU: 0.5, Memory: 30, Network: MaxInterval, Processes: 4.5}
	s.CPUMode = CPUTotal
	s.ProcessSort, s.ProcessOrder = SortName, OrderAscending
	s.ProcessFilter, s.ProcessView = ProcessesUser, ViewTree
	s.Colours.Grid = "#ff112233"
	s.Colours.SetCore(2, "#abcdef")
	s.Colours.Network["eth0"] = "#010203"
	s.HiddenAdapters = []string{"docker0", "lo"}
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	if got := Load(path); !reflect.DeepEqual(got, s) {
		t.Errorf("Load = %+v, want %+v", got, s)
	}
	// Nothing is left beside it.
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("the folder has %d files, want 1", len(entries))
	}
}

func TestSaveFails(t *testing.T) {
	if err := Defaults().Save(filepath.Join(t.TempDir(), "missing", "settings.json")); err == nil {
		t.Error("Save into a missing folder succeeded")
	}
	// A folder in the way cannot be replaced, and nothing is left beside it.
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.MkdirAll(filepath.Join(path, "inside"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Defaults().Save(path); err == nil {
		t.Error("Save over a folder succeeded")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("files left behind: %v", entries)
	}
}

func TestLoadNormalises(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	data := `{
		"width": 50, "height": 5000,
		"theme": "purple",
		"intervals": {"cpu": 0.04, "memory": 3601, "network": 7.26},
		"cpuMode": "sideways",
		"processSort": "colour", "processOrder": "sideways",
		"processFilter": "mine", "processView": "grid",
		"hiddenAdapters": ["veth1", "lo", "veth1"],
		"colours": {
			"grid": "grey", "total": "", "memory": "#12345", "swap": "#1234567",
			"cores": ["#111111", "nope", "#222222"],
			"network": {"eth0": "#333333", "wlan0": "bad"}
		}
	}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Load(path)
	d := Defaults()
	if s.Width != d.Width || s.Height != d.Height {
		t.Errorf("size = %dx%d, want the default", s.Width, s.Height)
	}
	if s.Theme != "" {
		t.Errorf("Theme = %q, want the system's", s.Theme)
	}
	// An interval that is missing, as that of processes is in the settings of
	// earlier versions, is the default.
	if want := (Intervals{CPU: DefaultIntervals.CPU, Memory: DefaultIntervals.Memory, Network: 7.3, Processes: DefaultIntervals.Processes}); s.Intervals != want {
		t.Errorf("Intervals = %+v, want %+v", s.Intervals, want)
	}
	if s.CPUMode != CPUCores {
		t.Errorf("CPUMode = %q", s.CPUMode)
	}
	if s.ProcessSort != SortCPU || s.ProcessOrder != OrderDescending {
		t.Errorf("process order = %q, %q, want the busiest first", s.ProcessSort, s.ProcessOrder)
	}
	if s.ProcessFilter != ProcessesUser || s.ProcessView != ViewList {
		t.Errorf("processes shown = %q, %q, want those of people as a list", s.ProcessFilter, s.ProcessView)
	}
	c := s.Colours
	if c.Grid != DefaultGrid || c.Total != DefaultTotal || c.Memory != DefaultMemory || c.Swap != DefaultSwap {
		t.Errorf("colours = %+v, want the defaults", c)
	}
	if want := []string{"#111111", "", "#222222"}; !reflect.DeepEqual(c.Cores, want) {
		t.Errorf("Cores = %v, want %v", c.Cores, want)
	}
	if want := map[string]string{"eth0": "#333333"}; !reflect.DeepEqual(c.Network, want) {
		t.Errorf("Network = %v, want %v", c.Network, want)
	}
	if want := []string{"lo", "veth1"}; !reflect.DeepEqual(s.HiddenAdapters, want) {
		t.Errorf("HiddenAdapters = %v, want %v", s.HiddenAdapters, want)
	}
}

func TestShown(t *testing.T) {
	s := Defaults()
	if !s.Shown("eth0") {
		t.Error("an adapter is hidden by default")
	}
	s.HiddenAdapters = []string{"lo"}
	if s.Shown("lo") || !s.Shown("eth0") {
		t.Error("Shown does not follow HiddenAdapters")
	}
}

func TestAdapterLabel(t *testing.T) {
	s := Defaults()
	s.AdapterAliases = map[string]string{"eth0": "Wired"}
	if s.AdapterLabel("eth0") != "Wired" || s.AdapterLabel("lo") != "lo" {
		t.Error("AdapterLabel does not follow AdapterAliases")
	}
}

func TestLoadAdapterAliases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"adapterAliases": {"eth0": " Wired ", "lo": "  "}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Load(path)
	if want := map[string]string{"eth0": "Wired"}; !reflect.DeepEqual(s.AdapterAliases, want) {
		t.Errorf("AdapterAliases = %v, want %v", s.AdapterAliases, want)
	}
}

func TestLoadNullNetwork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"colours": {"network": null}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if s := Load(path); s.Colours.Network == nil {
		t.Error("Network is nil, which an adapter cannot be added to")
	}
}

func TestValid(t *testing.T) {
	for colour, want := range map[string]bool{
		"#000000":     true,
		"#ABCdef":     true,
		"#80ff0000":   true,
		"":            false,
		"000000":      false,
		"#00000":      false,
		"#0000000":    false,
		"#gggggg":     false,
		"red":         false,
		"#ff00000000": false,
	} {
		if got := Valid(colour); got != want {
			t.Errorf("Valid(%q) = %v, want %v", colour, got, want)
		}
	}
}

func TestCore(t *testing.T) {
	var c Colours
	if got := c.Core(0); got != coreColours[0] {
		t.Errorf("Core(0) = %q", got)
	}
	c.SetCore(3, "#123456")
	if len(c.Cores) != 4 {
		t.Errorf("Cores = %v, want 4 long", c.Cores)
	}
	if got := c.Core(3); got != "#123456" {
		t.Errorf("Core(3) = %q", got)
	}
	if got := c.Core(1); got != coreColours[1] {
		t.Errorf("Core(1) = %q, want its default", got)
	}
	c.SetCore(0, "#654321")
	if got := c.Core(0); got != "#654321" || len(c.Cores) != 4 {
		t.Errorf("Core(0) = %q, Cores = %v", got, c.Cores)
	}
}

func TestDefaultCore(t *testing.T) {
	// The first processors have the colours of their own, and every one
	// after has a colour no other has.
	seen := map[string]bool{}
	for i := range 64 {
		colour := DefaultCore(i)
		if i < len(coreColours) && colour != coreColours[i] {
			t.Errorf("DefaultCore(%d) = %q, want %q", i, colour, coreColours[i])
		}
		if !Valid(colour) || seen[colour] {
			t.Errorf("DefaultCore(%d) = %q, not valid or not its own", i, colour)
		}
		seen[colour] = true
	}
}

func TestAdapter(t *testing.T) {
	var c Colours
	first, added := c.Adapter("eth0")
	if !added || first != adapterColours[0] {
		t.Errorf("Adapter(eth0) = %q, %v", first, added)
	}
	again, added := c.Adapter("eth0")
	if added || again != first {
		t.Errorf("Adapter(eth0) again = %q, %v", again, added)
	}
	second, _ := c.Adapter("wlan0")
	if second != adapterColours[1] {
		t.Errorf("Adapter(wlan0) = %q", second)
	}

	// A colour freed by an adapter that is forgotten is given again, and
	// one taken by a choice of the user's is not.
	delete(c.Network, "eth0")
	c.Network["docker0"] = variant(adapterColours, len(adapterColours))
	if got, _ := c.Adapter("veth1"); got != adapterColours[0] {
		t.Errorf("Adapter(veth1) = %q, want %q", got, adapterColours[0])
	}
	for _, name := range []string{"a", "b", "c", "d", "e", "f"} {
		c.Adapter(name)
	}
	// With every colour of their own taken, an adapter is given the first
	// variant that is free.
	want := variant(adapterColours, len(adapterColours)+1)
	if got, _ := c.Adapter("veth2"); got != want {
		t.Errorf("Adapter(veth2) = %q, want %q", got, want)
	}
}

func TestAdapterBeyondPalette(t *testing.T) {
	c := DefaultColours()
	seen := map[string]bool{}
	for i := range 40 {
		colour, _ := c.Adapter(string(rune('a' + i)))
		if seen[colour] {
			t.Fatalf("adapter %d was given %q, which another has", i, colour)
		}
		seen[colour] = true
	}
}

func TestVariant(t *testing.T) {
	palette := []string{"#ff0000", "#0000ff"}
	if variant(palette, 0) != "#ff0000" || variant(palette, 3) == "#0000ff" {
		t.Error("the palette is not taken in turn, and then varied")
	}
	// The next time round, a hue is turned one way, and the time after,
	// the other.
	h0, _, _ := toHSV(palette[1])
	h1, _, _ := toHSV(variant(palette, 3))
	h2, _, _ := toHSV(variant(palette, 5))
	if math.Abs(h1-(h0-10)) > 1 || math.Abs(h2-(h0+10)) > 1 {
		t.Errorf("hues turned to %v and %v from %v", h1, h2, h0)
	}
	// A pure blue, too dark for the dark background as bright as it goes,
	// is made paler to be seen on it.
	if c := variant(palette, 3); contrast(c, darkBackground) < minContrast || contrast(c, lightBackground) < minContrast {
		t.Errorf("variant %s is not seen on both backgrounds", c)
	}
}

func TestToHSV(t *testing.T) {
	for _, c := range []struct {
		colour  string
		h, s, v float64
	}{
		{"#000000", 0, 0, 0},
		{"#808080", 0, 0, 128.0 / 255},
		{"#ff0000", 0, 1, 1},
		{"#00ff00", 120, 1, 1},
		{"#0000ff", 240, 1, 1},
		{"#ff00ff", 300, 1, 1},
	} {
		h, s, v := toHSV(c.colour)
		if math.Abs(h-c.h) > 1e-9 || math.Abs(s-c.s) > 1e-9 || math.Abs(v-c.v) > 1e-9 {
			t.Errorf("toHSV(%s) = %v, %v, %v; want %v, %v, %v", c.colour, h, s, v, c.h, c.s, c.v)
		}
	}
	// It undoes hsv.
	for _, colour := range coreColours {
		r, g, b := hsv(toHSV(colour))
		if back := fmt.Sprintf("#%02x%02x%02x", r, g, b); back != colour {
			t.Errorf("%s went to HSV and back as %s", colour, back)
		}
	}
}

func TestContrast(t *testing.T) {
	if r := contrast("#000000", "#ffffff"); math.Abs(r-21) > 1e-9 {
		t.Errorf("contrast of black and white = %v, want 21", r)
	}
	if r := contrast("#268bd2", "#268bd2"); r != 1 {
		t.Errorf("contrast of a colour with itself = %v, want 1", r)
	}
}

func TestHSV(t *testing.T) {
	for _, c := range []struct {
		h       float64
		r, g, b uint8
	}{
		{0, 255, 0, 0},
		{60, 255, 255, 0},
		{120, 0, 255, 0},
		{180, 0, 255, 255},
		{240, 0, 0, 255},
		{300, 255, 0, 255},
		{330, 255, 0, 128},
	} {
		r, g, b := hsv(c.h, 1, 1)
		if r != c.r || g != c.g || b != c.b {
			t.Errorf("hsv(%v) = %d,%d,%d; want %d,%d,%d", c.h, r, g, b, c.r, c.g, c.b)
		}
	}
}

func TestPathFails(t *testing.T) {
	// Without a home or a configuration folder, there is nowhere to keep
	// the settings.
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if path, err := Path(); err == nil {
		t.Errorf("Path = %q, want an error", path)
	}
}

func TestLoadUnits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	for _, c := range []struct {
		json       string
		size, rate string
	}{
		{`{}`, SizeDecimal, RateBits},
		{`{"sizeUnits": "binary", "rateUnits": "bytes"}`, SizeBinary, RateBytes},
		{`{"sizeUnits": "huge", "rateUnits": "nibbles"}`, SizeDecimal, RateBits},
	} {
		if err := os.WriteFile(path, []byte(c.json), 0o600); err != nil {
			t.Fatal(err)
		}
		s := Load(path)
		if s.SizeUnits != c.size || s.RateUnits != c.rate {
			t.Errorf("%s: units = %q, %q, want %q, %q", c.json, s.SizeUnits, s.RateUnits, c.size, c.rate)
		}
	}
}

func TestClone(t *testing.T) {
	c := DefaultColours()
	c.Cores = []string{"#111111"}
	c.Network = map[string]string{"eth0": "#222222"}
	clone := c.Clone()
	clone.Cores[0] = "#333333"
	clone.Network["eth0"] = "#444444"
	clone.Network["lo"] = "#555555"
	if c.Cores[0] != "#111111" || c.Network["eth0"] != "#222222" || len(c.Network) != 1 {
		t.Errorf("changing the clone changed the colours: %+v", c)
	}
	// A clone of colours without adapters can be given them.
	empty := Colours{}.Clone()
	empty.Network["eth0"] = "#666666"
}

func TestPrepared(t *testing.T) {
	// The configuration of programs is kept in XDG_CONFIG_HOME on Linux, and
	// in the home folder on macOS, as TriggerAPI's tests have it.
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("HOME", config)
	path, err := Prepared()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, config) {
		t.Errorf("Prepared = %q, want it in %q", path, config)
	}
	if info, err := os.Stat(filepath.Dir(path)); err != nil || !info.IsDir() {
		t.Errorf("the folder of %q was not made: %v", path, err)
	}

	// A folder that cannot be made is an error.
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", file)
	t.Setenv("HOME", file)
	if path, err := Prepared(); err == nil {
		t.Errorf("Prepared = %q under a file, want an error", path)
	}

	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if path, err := Prepared(); err == nil {
		t.Errorf("Prepared = %q with nowhere to keep it, want an error", path)
	}
}

func TestDefaultsReadable(t *testing.T) {
	backgrounds := []string{lightBackground, darkBackground}
	// The accents stand out well from the background of a graph in the
	// light theme and in the dark one.
	for _, c := range coreColours {
		for _, background := range backgrounds {
			if r := contrast(c, background); r < 3 {
				t.Errorf("the accent %s on %s has a contrast of %.2f, want 3", c, background, r)
			}
		}
	}
	// Every other default colour of a line stands out enough from both,
	// however many processors and adapters there are.
	colours := []string{DefaultTotal, DefaultMemory, DefaultSwap}
	for i := range 64 {
		colours = append(colours, DefaultCore(i), variant(adapterColours, i))
	}
	for _, c := range colours {
		for _, background := range backgrounds {
			if r := contrast(c, background); r < minContrast {
				t.Errorf("%s on %s has a contrast of %.2f", c, background, r)
			}
		}
	}
}

func TestSaveFailsLeavesOldFile(t *testing.T) {
	dir := t.TempDir()
	// A folder where the file goes cannot be replaced by it, and the
	// temporary file the settings were written to is not left behind.
	path := filepath.Join(dir, "settings.json")
	if err := os.MkdirAll(filepath.Join(path, "inside"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Defaults().Save(path); err == nil {
		t.Error("Save over a folder succeeded")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("the folder has %d files, want only the one in the way", len(entries))
	}
}

func TestSaveReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := Defaults()
	s.Theme = ThemeDark
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	s.Theme = ThemeLight
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	if got := Load(path).Theme; got != ThemeLight {
		t.Errorf("Theme = %q after saving again, want %q", got, ThemeLight)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "}\n") {
		t.Error("the file does not end with a newline")
	}
}

func TestLoadRoundsIntervals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	for _, c := range []struct {
		json string
		want float64
	}{
		{`0.14`, 0.1},
		{`0.16`, 0.2},
		{`2.55`, 2.6},
		{`3600.04`, 3600},
		// Out of range, once rounded, or not a number, is the default.
		{`0.04`, DefaultIntervals.CPU},
		{`3600.06`, DefaultIntervals.CPU},
		{`-1`, DefaultIntervals.CPU},
		{`0`, DefaultIntervals.CPU},
		{`"5"`, DefaultIntervals.CPU},
	} {
		data := fmt.Sprintf(`{"intervals": {"cpu": %s, "memory": 1, "network": 1}}`, c.json)
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := Load(path).Intervals.CPU; got != c.want {
			t.Errorf("cpu interval %s = %v, want %v", c.json, got, c.want)
		}
	}
}

// TestLoadFrameRate checks that settings of earlier versions, which had a
// frame rate for the graphs, are read as they were but for it.
func TestLoadFrameRate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"frameRate": 0, "cpuMode": "total"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Load(path).CPUMode; got != CPUTotal {
		t.Errorf("CPUMode = %q, want the settings read", got)
	}
}

func TestShownSorted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	data := `{"hiddenAdapters": ["wlan0", "eth0", "wlan0"], "adapterAliases": {"eth0": "  ", "lo": " Loop "}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Load(path)
	if want := []string{"eth0", "wlan0"}; !reflect.DeepEqual(s.HiddenAdapters, want) {
		t.Errorf("HiddenAdapters = %v, want %v", s.HiddenAdapters, want)
	}
	if want := map[string]string{"lo": "Loop"}; !reflect.DeepEqual(s.AdapterAliases, want) {
		t.Errorf("AdapterAliases = %v, want %v", s.AdapterAliases, want)
	}
}
