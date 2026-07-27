// Package icon renders the tray icon at runtime.
//
// Generating the image in code rather than committing binary assets keeps the
// repository reviewable: a contributor can read what the icon is instead of
// trusting an opaque blob, and there is no build step to regenerate it.
package icon

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
)

const size = 32

// palette roughly matching Claude's warm accent, with a light glyph so the
// icon reads on both light and dark system trays.
var (
	bg    = color.NRGBA{R: 0xD9, G: 0x77, B: 0x57, A: 0xFF}
	fg    = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	blank = color.NRGBA{}
)

// render draws two overlapping rings, the switch-between-accounts metaphor.
func render() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))

	const r = float64(size)/2 - 0.5
	cx, cy := float64(size)/2-0.5, float64(size)/2-0.5

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := float64(x)-cx, float64(y)-cy
			d := math.Hypot(dx, dy)

			switch {
			case d > r:
				img.SetNRGBA(x, y, blank)
			case ring(dx+3.5, dy, 7.5, 2.0) || ring(dx-3.5, dy, 7.5, 2.0):
				img.SetNRGBA(x, y, fg)
			default:
				img.SetNRGBA(x, y, bg)
			}
		}
	}
	return img
}

// ring reports whether a point lies on a circle outline of the given radius
// and stroke width.
func ring(dx, dy, radius, width float64) bool {
	d := math.Hypot(dx, dy)
	return d >= radius-width/2 && d <= radius+width/2
}

// PNG returns the icon as PNG bytes, the format macOS and Linux trays expect.
func PNG() []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, render()); err != nil {
		return nil
	}
	return buf.Bytes()
}

// ICO returns the icon as a Windows .ico.
//
// Vista and later accept a PNG payload inside the ICO container, so the image
// only has to be encoded once.
func ICO() []byte {
	pngData := PNG()
	if pngData == nil {
		return nil
	}

	var buf bytes.Buffer
	// ICONDIR: reserved, type 1 (icon), one image.
	binary.Write(&buf, binary.LittleEndian, uint16(0))
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, uint16(1))

	// ICONDIRENTRY
	buf.WriteByte(size)                                           // width
	buf.WriteByte(size)                                           // height
	buf.WriteByte(0)                                              // palette colors
	buf.WriteByte(0)                                              // reserved
	binary.Write(&buf, binary.LittleEndian, uint16(1))            // color planes
	binary.Write(&buf, binary.LittleEndian, uint16(32))           // bits per pixel
	binary.Write(&buf, binary.LittleEndian, uint32(len(pngData))) // payload size
	binary.Write(&buf, binary.LittleEndian, uint32(6+16))         // payload offset

	buf.Write(pngData)
	return buf.Bytes()
}

// Data returns the icon in the format the current platform's tray expects.
func Data(goos string) []byte {
	if goos == "windows" {
		return ICO()
	}
	return PNG()
}
