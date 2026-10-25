// Package chart works out what a graph shows and where: the scale of its
// values, the labels of its axes, where each point of a line goes, and how a
// legend of many entries is laid out in rows. It does the arithmetic the UI
// draws by, and has no dependency on it.
package chart

import (
	"fmt"
	"math"
	"time"

	"github.com/nomad-software/prism/internal/history"
)

// Rows and Columns are how many parts the grid of a graph divides it into,
// up and across.
const (
	Rows    = 5
	Columns = 6
)

// FormatPercent writes a percentage to one decimal place.
func FormatPercent(p float64) string {
	return fmt.Sprintf("%.1f%%", p)
}

// Units are the units sizes and rates are written in.
type Units struct {
	// Decimal is for units of 1000 (kB, MB), rather than of 1024 (KiB,
	// MiB).
	Decimal bool
	// Bits is for rates in bits a second, rather than bytes.
	Bits bool
}

// The names of the units of bytes and of bits, each 1024 or 1000 times the
// last.
var (
	binaryBytes  = []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	decimalBytes = []string{"B", "kB", "MB", "GB", "TB", "PB", "EB"}
	binaryBits   = []string{"bits", "Kib", "Mib", "Gib", "Tib", "Pib", "Eib"}
	decimalBits  = []string{"bits", "kb", "Mb", "Gb", "Tb", "Pb", "Eb"}
)

// base is how many of a unit the next is.
func (u Units) base() float64 {
	if u.Decimal {
		return 1000
	}
	return 1024
}

// rateFactor is how many of what a rate is written in a byte is.
func (u Units) rateFactor() float64 {
	if u.Bits {
		return 8
	}
	return 1
}

// Size writes a number of bytes in the largest unit it has at least one of,
// to one decimal place but for a number of bytes alone.
func (u Units) Size(n float64) string {
	names := binaryBytes
	if u.Decimal {
		names = decimalBytes
	}
	return scaled(n, u.base(), names)
}

// Rate writes a number of bytes a second, in bytes or bits.
func (u Units) Rate(n float64) string {
	names := binaryBytes
	switch {
	case u.Bits && u.Decimal:
		names = decimalBits
	case u.Bits:
		names = binaryBits
	case u.Decimal:
		names = decimalBytes
	}
	return scaled(n*u.rateFactor(), u.base(), names) + "/s"
}

// scaled writes a number in the largest of the units named it has at least
// one of, each base times the last, to one decimal place but for a number
// of the first unit alone.
func scaled(n, base float64, names []string) string {
	if n < base {
		return fmt.Sprintf("%.0f %s", math.Max(n, 0), names[0])
	}
	i := 0
	for n >= base && i < len(names)-1 {
		n /= base
		i++
	}
	return fmt.Sprintf("%.1f %s", n, names[i])
}

// FormatAgo writes how long before now a line of the grid is, as the labels
// under a graph do: "1 min", "50 secs", "2 hours".
func FormatAgo(d time.Duration) string {
	plural := func(n int64, unit string) string {
		if n == 1 {
			return fmt.Sprintf("%d %s", n, unit)
		}
		return fmt.Sprintf("%d %ss", n, unit)
	}
	switch {
	case d >= time.Hour && d%time.Hour == 0:
		return plural(int64(d/time.Hour), "hour")
	case d >= time.Minute && d%time.Minute == 0:
		return plural(int64(d/time.Minute), "min")
	default:
		return plural(int64(d.Round(time.Second)/time.Second), "sec")
	}
}

// TimeLabels returns the labels of the lines of the grid down a graph that
// spans the time, from its left edge to the last before its right, which is
// now and has none.
func TimeLabels(span time.Duration) []string {
	labels := make([]string, Columns)
	for i := range Columns {
		labels[i] = FormatAgo(span * time.Duration(Columns-i) / Columns)
	}
	return labels
}

// PercentLabels returns the labels of the lines of the grid across a graph
// of percentages, from the bottom, which is 0%, to the top, which is 100%.
func PercentLabels() []string {
	labels := make([]string, Rows+1)
	for i := range labels {
		labels[i] = fmt.Sprintf("%d %%", i*100/Rows)
	}
	return labels
}

// RateTop returns the value the top of a graph of rates is, in bytes a
// second, for the largest rate it shows: the least of 1, 2 and 5 times a
// power of ten of the unit the rate is written in that is no less than it,
// and no less than 1 kilo of the units, so that a graph of an idle adapter
// is not a scale of a few bytes.
func (u Units) RateTop(largest float64) float64 {
	base, factor := u.base(), u.rateFactor()
	largest *= factor
	if largest <= base {
		return base / factor
	}
	unit := 1.0
	for largest/unit >= base {
		unit *= base
	}
	n := largest / unit
	for power := 1.0; ; power *= 10 {
		for _, step := range []float64{1, 2, 5} {
			if step*power >= n {
				return step * power * unit / factor
			}
		}
	}
}

// RateLabels returns the labels of the lines of the grid across a graph of
// rates whose top is the value, from the bottom to the top.
func (u Units) RateLabels(top float64) []string {
	labels := make([]string, Rows+1)
	for i := range labels {
		labels[i] = u.Rate(top * float64(i) / Rows)
	}
	return labels
}

// SizeLabels returns the labels of the lines of the grid across a graph of
// sizes whose top is the value, from the bottom to the top.
func (u Units) SizeLabels(top float64) []string {
	labels := make([]string, Rows+1)
	for i := range labels {
		labels[i] = u.Size(top * float64(i) / Rows)
	}
	return labels
}

// Rect is a rectangle, where a graph is drawn.
type Rect struct {
	X, Y, W, H float64
}

// XY is a place in a graph.
type XY struct {
	X, Y float64
}

// Place returns where each point goes in a rectangle that spans the time up
// to now across it, and the values from 0 to top up it. Values beyond the
// top are put at it, and the time of a point before the left edge is left
// as it is, as it is drawn from to reach the edge and is cut off there.
func Place(points []history.Point, now time.Time, span time.Duration, top float64, r Rect) []XY {
	if top <= 0 {
		top = 1
	}
	placed := make([]XY, len(points))
	for i, p := range points {
		ago := now.Sub(p.T).Seconds()
		v := math.Min(math.Max(p.V, 0), top)
		placed[i] = XY{
			X: r.X + r.W - ago/span.Seconds()*r.W,
			Y: r.Y + r.H - v/top*r.H,
		}
	}
	return placed
}

// Controls returns the two control points of the curve between two points,
// half way between them across, each at the height of the point it is
// nearer. The curve is smooth where it meets the next, and never goes above
// or below its points, so it never shows a value that was not measured.
func Controls(from, to XY) (XY, XY) {
	mid := (from.X + to.X) / 2
	return XY{mid, from.Y}, XY{mid, to.Y}
}

// Grid is how a legend lays out its entries: in columns of equal width, row
// by row.
type Grid struct {
	Columns, Rows int
}

// Layout returns how many entries of the width go across the width there is,
// with the gap between them, and how many rows they take. There is always at
// least one column, even where an entry is wider than the width, and entries
// of no width all go in one row.
func Layout(entries int, entry, gap, width float64) Grid {
	if entries <= 0 {
		return Grid{}
	}
	columns := entries
	if entry+gap > 0 {
		columns = max(1, int((width+gap)/(entry+gap)))
	}
	columns = min(columns, entries)
	return Grid{Columns: columns, Rows: (entries + columns - 1) / columns}
}

// Cell returns the column and row of the entry at the index.
func (g Grid) Cell(i int) (column, row int) {
	if g.Columns == 0 {
		return 0, 0
	}
	return i % g.Columns, i / g.Columns
}
