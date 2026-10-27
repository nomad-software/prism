// Package appicon holds the program's icon, built into the binary so that the
// program needs nothing beside it, and draws it: a prism, a triangle on a
// clear background divided into thirds, red, blue and green. It has no
// dependency on the UI.
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

// The colours of the thirds of the triangle, each that of the corner it lies
// around: the top, the bottom left and the bottom right. They are
// Solarized's red, blue and green, as the graphs' default colours are
// Solarized's.
var thirds = []color.RGBA{
	{0xdc, 0x32, 0x2f, 0xff},
	{0x26, 0x8b, 0xd2, 0xff},
	{0x85, 0x99, 0x00, 0xff},
}

// point is a place on the icon, from 0 to 1 across and down it.
type point struct{ x, y float64 }

// The triangle is equilateral, its sides side long and in the middle of the
// icon, with its top corner first, then the bottom left and bottom right, in
// the order of the thirds.
const side = 0.96

var corners = func() [3]point {
	height := side * math.Sqrt(3) / 2
	top := (1 - height) / 2
	return [3]point{{0.5, top}, {0.5 - side/2, top + height}, {0.5 + side/2, top + height}}
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

// shade returns the colour at a place on the icon, clear outside the
// triangle.
func shade(x, y float64) color.RGBA {
	p := point{x, y}
	if !inTriangle(p, corners) {
		return color.RGBA{}
	}
	// The thirds are parted by lines from the middle of the triangle to the
	// middle of each side, with a thin clear gap along each. Each line is
	// halfway between the two corners at the ends of its side, so a place
	// is in the third of the corner nearest it.
	const gap = 0.012
	centre := point{(corners[0].x + corners[1].x + corners[2].x) / 3, (corners[0].y + corners[1].y + corners[2].y) / 3}
	nearest, distance := 0, math.Inf(1)
	for i, c := range corners {
		next := corners[(i+1)%3]
		if distanceToSegment(p, centre, point{(c.x + next.x) / 2, (c.y + next.y) / 2}) <= gap {
			return color.RGBA{}
		}
		if d := math.Hypot(p.x-c.x, p.y-c.y); d < distance {
			nearest, distance = i, d
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
