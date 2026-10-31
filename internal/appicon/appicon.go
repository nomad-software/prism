// Package appicon holds the program's icon, built into the binary so that the
// program needs nothing beside it. It has no dependency on the UI.
package appicon

import _ "embed"

// PNG is the program's icon, a PNG Size pixels square.
//
//go:embed appicon.png
var PNG []byte

// Size is the width and height of PNG.
const Size = 128
