package ui

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/nomad-software/prism/internal/appicon"
)

// iconsEnv names the folder TestWriteIcons writes the icon of the macOS app
// into. The test is skipped without it; make icons sets it to packaging.
const iconsEnv = "PRISM_ICONS"

// macIconSize and macIconMargin are the size of the icon of the macOS app and
// the margin Apple's grid gives the shape of an icon inside it, which the
// icons of the Dock are drawn with.
const (
	macIconSize   = 1024
	macIconMargin = 100
)

// macIconPNG returns the program's icon as the macOS app has it: scaled
// smoothly into the middle of a transparent square of the size, within the
// margin.
func macIconPNG(size, margin int) []byte {
	icon := qt.NewQImage()
	defer icon.Delete()
	if !icon.LoadFromDataWithData(appicon.PNG) {
		return nil
	}
	inner := size - 2*margin
	// A value miqt frees itself once it is no longer used; its picture is
	// let go of here, once it has been drawn, rather than by its finalizer
	// off the main thread.
	scaled := icon.Scaled3(inner, inner, qt.KeepAspectRatio, qt.SmoothTransformation)
	defer releaseImage(scaled)

	out := qt.NewQImage3(size, size, qt.QImage__Format_ARGB32_Premultiplied)
	defer out.Delete()
	out.Fill2(qt.Transparent)
	painter := qt.NewQPainter2(out.QPaintDevice)
	painter.SetRenderHint(qt.QPainter__SmoothPixmapTransform)
	painter.DrawImage9(margin+(inner-scaled.Width())/2, margin+(inner-scaled.Height())/2, scaled)
	painter.End()
	painter.Delete()

	buffer := qt.NewQBuffer()
	defer buffer.Delete()
	if !buffer.Open(qt.QIODeviceBase__WriteOnly) || !out.Save4(buffer.QIODevice, "PNG") {
		return nil
	}
	return buffer.Data()
}

// TestWriteIcons draws the icon the macOS app is made from,
// packaging/macos/prism.png, from which macos.sh makes the .icns of the app.
// The AppImage has the program's own icon as it is. Run make icons again
// whenever the icon changes.
func TestWriteIcons(t *testing.T) {
	dir := os.Getenv(iconsEnv)
	if dir == "" {
		t.Skip(iconsEnv + " is not set")
	}
	var data []byte
	mainthread.Wait(func() { data = macIconPNG(macIconSize, macIconMargin) })
	if data == nil {
		t.Fatal("the icon could not be drawn")
	}
	if err := os.WriteFile(filepath.Join(dir, "macos", "prism.png"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestMacIcon checks that the icon of the macOS app has its margin clear and
// is the one drawn from the program's icon as it is, so that a new icon is not
// left out of the app.
func TestMacIcon(t *testing.T) {
	var data []byte
	mainthread.Wait(func() { data = macIconPNG(macIconSize, macIconMargin) })
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil || img.Bounds().Dx() != macIconSize || img.Bounds().Dy() != macIconSize {
		t.Fatalf("icon %v, %v", img, err)
	}
	if _, _, _, a := img.At(macIconMargin/2, macIconSize/2).RGBA(); a != 0 {
		t.Error("the margin is not clear")
	}
	if _, _, _, a := img.At(macIconSize/2, macIconSize/2).RGBA(); a == 0 {
		t.Error("the middle is clear")
	}

	kept, err := os.ReadFile(filepath.Join("..", "..", "packaging", "macos", "prism.png"))
	if err != nil {
		t.Fatal(err)
	}
	keptImg, err := png.Decode(bytes.NewReader(kept))
	if err != nil || keptImg.Bounds() != img.Bounds() {
		t.Fatalf("packaging/macos/prism.png is %v, %v: run make icons", keptImg, err)
	}
	// Another Qt may scale or round a little differently, which is not a
	// different icon.
	if d := iconDifference(img, keptImg); d > 0x400 {
		t.Errorf("packaging/macos/prism.png differs by %d on average: run make icons", d)
	}
}

// iconDifference returns how far apart the channels of two images of one size
// are on average, sampling every 8th pixel, from 0 to 0xffff.
func iconDifference(a, b image.Image) uint64 {
	abs := func(x, y uint32) uint64 {
		if x > y {
			return uint64(x - y)
		}
		return uint64(y - x)
	}
	var total, count uint64
	r := a.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y += 8 {
		for x := r.Min.X; x < r.Max.X; x += 8 {
			ar, ag, ab, aa := a.At(x, y).RGBA()
			br, bg, bb, ba := b.At(x, y).RGBA()
			total += abs(ar, br) + abs(ag, bg) + abs(ab, bb) + abs(aa, ba)
			count += 4
		}
	}
	return total / count
}
