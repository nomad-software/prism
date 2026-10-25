package chart

import (
	"reflect"
	"testing"
	"time"

	"github.com/nomad-software/prism/internal/history"
)

func TestFormatPercent(t *testing.T) {
	for v, want := range map[float64]string{0: "0.0%", 8.84: "8.8%", 65: "65.0%", 100: "100.0%"} {
		if got := FormatPercent(v); got != want {
			t.Errorf("FormatPercent(%v) = %q, want %q", v, got, want)
		}
	}
}

func TestSize(t *testing.T) {
	for _, c := range []struct {
		n    float64
		want string
	}{
		{-5, "0 B"},
		{0, "0 B"},
		{394, "394 B"},
		{1023, "1023 B"},
		{1024, "1.0 KiB"},
		{1126, "1.1 KiB"},
		{1.5 * 1024 * 1024, "1.5 MiB"},
		{4.2 * 1024 * 1024 * 1024, "4.2 GiB"},
		{3 * 1024 * 1024 * 1024 * 1024, "3.0 TiB"},
		{2048 * 1024 * 1024 * 1024 * 1024 * 1024 * 1024, "2048.0 EiB"},
	} {
		if got := (Units{}).Size(c.n); got != c.want {
			t.Errorf("Size(%v) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestRate(t *testing.T) {
	for _, c := range []struct {
		units Units
		n     float64
		want  string
	}{
		{Units{}, 2048, "2.0 KiB/s"},
		{Units{Decimal: true}, 2048, "2.0 kB/s"},
		{Units{Bits: true}, 2048, "16.0 Kib/s"},
		{Units{Decimal: true, Bits: true}, 125000, "1.0 Mb/s"},
		{Units{Bits: true}, 100, "800 bits/s"},
	} {
		if got := c.units.Rate(c.n); got != c.want {
			t.Errorf("%+v.Rate(%v) = %q, want %q", c.units, c.n, got, c.want)
		}
	}
	if got := (Units{Decimal: true}).Size(1500); got != "1.5 kB" {
		t.Errorf("decimal Size = %q", got)
	}
}

func TestFormatAgo(t *testing.T) {
	for _, c := range []struct {
		d    time.Duration
		want string
	}{
		{time.Second, "1 sec"},
		{10 * time.Second, "10 secs"},
		{50 * time.Second, "50 secs"},
		{time.Minute, "1 min"},
		{90 * time.Second, "90 secs"},
		{20 * time.Minute, "20 mins"},
		{time.Hour, "1 hour"},
		{2 * time.Hour, "2 hours"},
		{1500 * time.Millisecond, "2 secs"},
	} {
		if got := FormatAgo(c.d); got != c.want {
			t.Errorf("FormatAgo(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestTimeLabels(t *testing.T) {
	want := []string{"1 min", "50 secs", "40 secs", "30 secs", "20 secs", "10 secs"}
	if got := TimeLabels(time.Minute); !reflect.DeepEqual(got, want) {
		t.Errorf("TimeLabels(1m) = %v, want %v", got, want)
	}
	want = []string{"2 hours", "100 mins", "80 mins", "1 hour", "40 mins", "20 mins"}
	if got := TimeLabels(2 * time.Hour); !reflect.DeepEqual(got, want) {
		t.Errorf("TimeLabels(2h) = %v, want %v", got, want)
	}
}

func TestPercentLabels(t *testing.T) {
	want := []string{"0 %", "20 %", "40 %", "60 %", "80 %", "100 %"}
	if got := PercentLabels(); !reflect.DeepEqual(got, want) {
		t.Errorf("PercentLabels = %v, want %v", got, want)
	}
}

func TestRateTop(t *testing.T) {
	const k, m = 1024.0, 1024.0 * 1024
	for _, c := range []struct{ largest, want float64 }{
		{0, k},
		{500, k},
		{k, k},
		{1.5 * k, 2 * k},
		{3 * k, 5 * k},
		{7 * k, 10 * k},
		{150 * k, 200 * k},
		{600 * k, 1000 * k},
		{1000 * k, 1000 * k},
		{1010 * k, 2000 * k},
		{1.2 * m, 2 * m},
		{90 * m, 100 * m},
	} {
		if got := (Units{}).RateTop(c.largest); got != c.want {
			t.Errorf("RateTop(%v) = %v, want %v", c.largest, got, c.want)
		}
	}
}

func TestRateTopBits(t *testing.T) {
	// 1.5 Mb/s, in bytes a second, is drawn to 2 Mb/s.
	u := Units{Decimal: true, Bits: true}
	if got := u.RateTop(1.5e6 / 8); got != 2e6/8 {
		t.Errorf("RateTop = %v, want %v", got, 2e6/8)
	}
	// An idle adapter is drawn to 1 kb/s.
	if got := u.RateTop(0); got != 1000.0/8 {
		t.Errorf("RateTop(0) = %v", got)
	}
}

func TestSizeLabels(t *testing.T) {
	want := []string{"0 B", "3.2 GiB", "6.4 GiB", "9.6 GiB", "12.8 GiB", "16.0 GiB"}
	if got := (Units{}).SizeLabels(16 << 30); !reflect.DeepEqual(got, want) {
		t.Errorf("SizeLabels = %v, want %v", got, want)
	}
	want = []string{"0 B", "3.2 GB", "6.4 GB", "9.6 GB", "12.8 GB", "16.0 GB"}
	if got := (Units{Decimal: true}).SizeLabels(16e9); !reflect.DeepEqual(got, want) {
		t.Errorf("decimal SizeLabels = %v, want %v", got, want)
	}
}

func TestRateLabels(t *testing.T) {
	want := []string{"0 B/s", "409.6 KiB/s", "819.2 KiB/s", "1.2 MiB/s", "1.6 MiB/s", "2.0 MiB/s"}
	if got := (Units{}).RateLabels(2 * 1024 * 1024); !reflect.DeepEqual(got, want) {
		t.Errorf("RateLabels = %v, want %v", got, want)
	}
}

func TestPlace(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	points := []history.Point{
		{T: now.Add(-90 * time.Second), V: 50},
		{T: now.Add(-30 * time.Second), V: 150},
		{T: now, V: -5},
	}
	r := Rect{X: 10, Y: 20, W: 600, H: 100}
	got := Place(points, now, time.Minute, 100, r)
	want := []XY{
		{X: 10 - 300, Y: 70},
		{X: 310, Y: 20},
		{X: 610, Y: 120},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Place = %v, want %v", got, want)
	}

	// A top of nothing is taken as 1, rather than dividing by it.
	got = Place([]history.Point{{T: now, V: 1}}, now, time.Minute, 0, r)
	if got[0].Y != 20 {
		t.Errorf("Place with no top = %v", got)
	}
}

func TestControls(t *testing.T) {
	a, b := Controls(XY{0, 10}, XY{10, 30})
	if a != (XY{5, 10}) || b != (XY{5, 30}) {
		t.Errorf("Controls = %v, %v", a, b)
	}
}

func TestLayout(t *testing.T) {
	for _, c := range []struct {
		name              string
		entries           int
		entry, gap, width float64
		want              Grid
	}{
		{"none", 0, 100, 10, 500, Grid{}},
		{"eight in four", 8, 100, 10, 440, Grid{4, 2}},
		{"exact fit", 4, 100, 10, 430, Grid{4, 1}},
		{"just short", 4, 100, 10, 429, Grid{3, 2}},
		{"too narrow", 5, 100, 10, 50, Grid{1, 5}},
		{"fewer than fit", 3, 100, 10, 1000, Grid{3, 1}},
		{"thirty two", 32, 150, 20, 1000, Grid{6, 6}},
		{"no width", 2, 0, 0, 100, Grid{2, 1}},
	} {
		if got := Layout(c.entries, c.entry, c.gap, c.width); got != c.want {
			t.Errorf("%s: Layout = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestCell(t *testing.T) {
	g := Grid{Columns: 4, Rows: 2}
	for i, want := range [][2]int{{0, 0}, {1, 0}, {2, 0}, {3, 0}, {0, 1}, {1, 1}} {
		c, r := g.Cell(i)
		if c != want[0] || r != want[1] {
			t.Errorf("Cell(%d) = %d, %d; want %v", i, c, r, want)
		}
	}
	if c, r := (Grid{}).Cell(3); c != 0 || r != 0 {
		t.Errorf("Cell of an empty grid = %d, %d", c, r)
	}
}
