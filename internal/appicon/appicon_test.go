package appicon

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// TestWriteIcons draws the icons into the folder PRISM_ICONS names,
// as make icons has it: the program's icon, internal/appicon/appicon.png,
// and that of the macOS app, packaging/macos/prism.png. It does
// nothing without it.
func TestWriteIcons(t *testing.T) {
	root := os.Getenv("PRISM_ICONS")
	if root == "" {
		t.Skip("PRISM_ICONS is not set")
	}
	write := func(path string, img image.Image) {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, path), buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("internal/appicon/appicon.png", Draw(Size, 0))
	write("packaging/macos/prism.png", Draw(1024, MacMargin))
}

// TestPNG checks the icon built in is the one Draw draws, so that it is
// drawn again (make icons) whenever the drawing changes.
func TestPNG(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(PNG))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != Size || b.Dy() != Size {
		t.Fatalf("the icon is %v, want %d square", b, Size)
	}
	if !bytes.Equal(nrgba(img).Pix, Draw(Size, 0).Pix) {
		t.Error("internal/appicon/appicon.png is not drawn as it is now: run make icons")
	}
}

// TestMacIcon checks the icon of the macOS app is drawn as it is now.
func TestMacIcon(t *testing.T) {
	data, err := os.ReadFile("../../packaging/macos/prism.png")
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(nrgba(img).Pix, Draw(1024, MacMargin).Pix) {
		t.Error("packaging/macos/prism.png is not drawn as it is now: run make icons")
	}
}

// nrgba returns the image as NRGBA, as Draw draws it.
func nrgba(img image.Image) *image.NRGBA {
	if r, ok := img.(*image.NRGBA); ok {
		return r
	}
	r := image.NewNRGBA(img.Bounds())
	for y := r.Rect.Min.Y; y < r.Rect.Max.Y; y++ {
		for x := r.Rect.Min.X; x < r.Rect.Max.X; x++ {
			r.Set(x, y, img.At(x, y))
		}
	}
	return r
}

func TestDraw(t *testing.T) {
	img := Draw(64, 0)
	// The corners of the icon are clear, and the middle of each third
	// solid.
	if a := img.NRGBAAt(0, 0).A; a != 0 {
		t.Errorf("a corner is %d opaque, want clear", a)
	}
	if a := img.NRGBAAt(20, 56).A; a != 0xff {
		t.Errorf("the bottom left third is %d opaque, want solid", a)
	}
	withMargin := Draw(64, 0.25)
	if a := withMargin.NRGBAAt(10, 46).A; a != 0 {
		t.Errorf("the margin is %d opaque, want clear", a)
	}
	if a := withMargin.NRGBAAt(24, 44).A; a != 0xff {
		t.Errorf("the bottom left third within the margin is %d opaque, want solid", a)
	}
}

func TestPixel(t *testing.T) {
	if c := pixel(0, 0, 10, 100); c != (color.NRGBA{}) {
		t.Errorf("a pixel in the margin = %v, want clear", c)
	}
	if c := pixel(30, 85, 0, 100); c != color.NRGBAModel.Convert(thirds[1]) {
		t.Errorf("a pixel of the bottom left third = %v, want %v", c, thirds[1])
	}
	// A pixel on the bottom edge is part clear, and keeps its colour
	// rather than being darkened.
	y := int(corners[1].y * 100)
	c := pixel(30, y, 0, 100)
	if c.A == 0 || c.A == 0xff || c.B != thirds[1].B {
		t.Errorf("a pixel of the edge = %v, want part clear and the bottom left third's colour", c)
	}
}

func TestShade(t *testing.T) {
	centre := (corners[0].y + corners[1].y + corners[2].y) / 3
	for _, c := range []struct {
		name string
		x, y float64
		want color.RGBA
	}{
		{"outside", 0.05, 0.1, color.RGBA{}},
		{"top third", 0.5, centre - 0.1, thirds[0]},
		{"bottom left third", 0.3, corners[1].y - 0.05, thirds[1]},
		{"bottom right third", 0.7, corners[1].y - 0.05, thirds[2]},
		{"gap", 0.5, (centre + corners[1].y) / 2, color.RGBA{}},
	} {
		if got := shade(c.x, c.y); got != c.want {
			t.Errorf("%s: shade = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestInTriangle(t *testing.T) {
	c := [3]point{{0.5, 0}, {0, 1}, {1, 1}}
	for _, p := range []struct {
		p    point
		want bool
	}{
		{point{0.5, 0.5}, true},
		{point{0.5, 1}, true},
		{point{0.1, 0.1}, false},
		{point{0.5, 1.01}, false},
	} {
		if got := inTriangle(p.p, c); got != p.want {
			t.Errorf("inTriangle(%v) = %v", p.p, got)
		}
	}
}

func TestDistance(t *testing.T) {
	for _, c := range []struct {
		p, a, b point
		want    float64
	}{
		{point{0.5, 1}, point{0, 0}, point{1, 0}, 1},
		{point{-1, 0}, point{0, 0}, point{1, 0}, 1},
		{point{2, 0}, point{0, 0}, point{1, 0}, 1},
		{point{3, 4}, point{0, 0}, point{0, 0}, 5},
	} {
		if got := distanceToSegment(c.p, c.a, c.b); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("distanceToSegment(%v, %v, %v) = %v, want %v", c.p, c.a, c.b, got, c.want)
		}
	}
}
