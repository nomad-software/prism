package appicon

import (
	"bytes"
	"image/png"
	"testing"
)

// TestPNG checks the icon built in can be read and is the size it is said to
// be.
func TestPNG(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(PNG))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != size || b.Dy() != size {
		t.Fatalf("the icon is %v, want %d square", b, size)
	}
}
