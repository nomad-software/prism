// Package appicon holds the program's icon, built into the binary so that the
// program needs nothing beside it, and draws it: a prism, a triangle on a dark
// tile divided into thirds, red, green and blue. It has no dependency on the
// UI.
package appicon

import (
	_ "embed"
	"image"
	"image/color"
	"math"
	"runtime"
	"sync"
)

// PNG is the program's icon, a 256×256 PNG drawn by Draw (make icons).
//
//go:embed appicon.png
var PNG []byte

// Size is the width and height of PNG.
const Size = 256

// MacMargin is the part of each side of the icon of the macOS app left
// empty, as Apple's grid has icons 824 of 1024 wide.
const MacMargin = 100.0 / 1024

// The colours of the icon: the tile, and the thirds of the triangle, each
// that of the corner it holds.
var (
	tile   = color.RGBA{0x2d, 0x2d, 0x2d, 0xff}
	thirds = []color.RGBA{
		{0xe8, 0x2e, 0x2e, 0xff},
		{0x2e, 0xc2, 0x4a, 0xff},
		{0x2e, 0x6e, 0xe8, 0xff},
	}
)

// point is a place on the tile, from 0 to 1 across and down it.
type point struct{ x, y float64 }

// The triangle is equilateral, its sides side long, with its top corner
// first, then the bottom left and bottom right, as thirds has them.
const (
	side = 0.78
	top  = 0.14
)

var corners = func() [3]point {
	bottom := top + side*math.Sqrt(3)/2
	return [3]point{{0.5, top}, {0.5 - side/2, bottom}, {0.5 + side/2, bottom}}
}()

// samples is how many samples each pixel is made of, across and down, to
// smooth the edges of what is drawn.
const samples = 4

// Draw draws the icon, size pixels square, with the margin, a part of its
// size, left empty on each side. Its rows are drawn side by side, on as many
// threads as there are processors.
func Draw(size int, margin float64) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	inner := float64(size) * (1 - 2*margin)
	offset := float64(size) * margin
	rows := make(chan int)
	var wg sync.WaitGroup
	for range runtime.NumCPU() {
		wg.Go(func() {
			for py := range rows {
				for px := range size {
					img.SetNRGBA(px, py, pixel(px, py, offset, inner))
				}
			}
		})
	}
	for py := range size {
		rows <- py
	}
	close(rows)
	wg.Wait()
	return img
}

// pixel returns the colour of a pixel, the average of the samples of it.
func pixel(px, py int, offset, inner float64) color.NRGBA {
	var r, g, b, a float64
	for sy := range samples {
		for sx := range samples {
			x := (float64(px) + (float64(sx)+0.5)/samples - offset) / inner
			y := (float64(py) + (float64(sy)+0.5)/samples - offset) / inner
			c := shade(x, y)
			// The colours are made premultiplied by how opaque they are,
			// so that an average of them is right.
			alpha := float64(c.A) / 0xff
			r += float64(c.R) * alpha
			g += float64(c.G) * alpha
			b += float64(c.B) * alpha
			a += float64(c.A)
		}
	}
	if a == 0 {
		return color.NRGBA{}
	}
	// What was premultiplied is divided by how opaque it is again.
	opacity := a / 0xff
	return color.NRGBA{
		R: uint8(math.Round(r / opacity)),
		G: uint8(math.Round(g / opacity)),
		B: uint8(math.Round(b / opacity)),
		A: uint8(math.Round(a / (samples * samples))),
	}
}

// shade returns the colour at a place on the tile.
func shade(x, y float64) color.RGBA {
	const radius = 0.2
	if !inRoundedSquare(x, y, radius) {
		return color.RGBA{}
	}
	p := point{x, y}
	if !inTriangle(p, corners) {
		return tile
	}
	// The thirds are parted by lines from the middle of the triangle to the
	// middle of each side, with a thin gap of the tile along each.
	const gap = 0.012
	centre := point{(corners[0].x + corners[1].x + corners[2].x) / 3, (corners[0].y + corners[1].y + corners[2].y) / 3}
	for i := range corners {
		a, b := corners[i], corners[(i+1)%3]
		if distanceToSegment(p, centre, point{(a.x + b.x) / 2, (a.y + b.y) / 2}) <= gap {
			return tile
		}
	}
	// Each third of an equilateral triangle so parted is the part nearer
	// its corner than the others.
	nearest := 0
	for i := range corners {
		if math.Hypot(x-corners[i].x, y-corners[i].y) < math.Hypot(x-corners[nearest].x, y-corners[nearest].y) {
			nearest = i
		}
	}
	return thirds[nearest]
}

// inTriangle says whether a place is in the triangle with the corners, by
// its being on the same side of each of the three sides.
func inTriangle(p point, c [3]point) bool {
	cross := func(a, b point) float64 {
		return (b.x-a.x)*(p.y-a.y) - (b.y-a.y)*(p.x-a.x)
	}
	d1, d2, d3 := cross(c[0], c[1]), cross(c[1], c[2]), cross(c[2], c[0])
	return (d1 >= 0 && d2 >= 0 && d3 >= 0) || (d1 <= 0 && d2 <= 0 && d3 <= 0)
}

// inRoundedSquare says whether a place is in the unit square with its
// corners rounded to the radius.
func inRoundedSquare(x, y, radius float64) bool {
	if x < 0 || x > 1 || y < 0 || y > 1 {
		return false
	}
	cx := math.Max(radius, math.Min(x, 1-radius))
	cy := math.Max(radius, math.Min(y, 1-radius))
	return math.Hypot(x-cx, y-cy) <= radius
}

// distanceToSegment returns how far a place is from the nearest part of the
// line from a to b.
func distanceToSegment(p, a, b point) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	t := 0.0
	if l := dx*dx + dy*dy; l > 0 {
		t = math.Max(0, math.Min(1, ((p.x-a.x)*dx+(p.y-a.y)*dy)/l))
	}
	return math.Hypot(p.x-(a.x+t*dx), p.y-(a.y+t*dy))
}
