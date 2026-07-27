// Package icon provides the application icon for the tray app.
//
// The artwork is the project's official icon, embedded at several sizes rather
// than scaled at runtime. Pre-rendering matters for a tray icon: it is drawn at
// 16 or 32 pixels, where a general-purpose scaler applied to a detailed source
// image produces visible mush. Each size here was downsampled once, with high
// quality filtering, from the 1254px original in assets/icon.png.
//
// The source artwork is deliberately opaque. It is a dark tile with the mark on
// top, so it reads as a badge in the tray rather than as floating shapes, and
// it stays legible on both light and dark taskbars.
package icon

import (
	"bytes"
	_ "embed"
	"encoding/binary"
)

//go:embed data/icon-16.png
var png16 []byte

//go:embed data/icon-32.png
var png32 []byte

//go:embed data/icon-48.png
var png48 []byte

//go:embed data/icon-64.png
var png64 []byte

//go:embed data/icon-128.png
var png128 []byte

// variant is one embedded rendering.
type variant struct {
	size int
	data []byte
}

// variants must stay sorted ascending: the ICO directory is written in this
// order, and Windows picks an entry by matching the requested size.
//
// It stops at 128 on purpose. A tray icon is requested at 16 to 48 depending on
// display scaling, and 128 already covers Alt-Tab and the task manager. A 256px
// entry was measured at 63 KB, seventy percent of the whole container, for an
// image nothing here asks for. GDI+ also declines to return PNG-compressed
// 256px entries, handing back the 128 one instead, so including it would have
// added weight and a compatibility wrinkle in exchange for nothing.
var variants = []variant{
	{16, png16},
	{32, png32},
	{48, png48},
	{64, png64},
	{128, png128},
}

// PNG returns the icon as PNG bytes, the format macOS and Linux trays expect.
//
// 64 pixels is the useful default: large enough for a HiDPI menu bar, small
// enough that the toolkit's own downscale to the bar height stays sharp.
func PNG() []byte { return png64 }

// PNGSize returns the embedded rendering closest to size, at least as large as
// it where one exists, so callers never scale up.
func PNGSize(size int) []byte {
	for _, v := range variants {
		if v.size >= size {
			return v.data
		}
	}
	return variants[len(variants)-1].data
}

// ICO returns a multi-resolution Windows icon containing every embedded size.
//
// Shipping all sizes in one container lets Windows choose per context: 16 for
// the tray at 100% scaling, 32 at 200%, larger again for Alt-Tab and the task
// manager. A single-size ICO would be rescaled by the shell instead, which is
// exactly the blurring the pre-rendered sizes exist to avoid.
//
// Vista and later accept a PNG payload inside the ICO container, so each entry
// embeds its PNG directly rather than a BMP with a separate AND mask.
func ICO() []byte {
	const (
		headerLen = 6
		entryLen  = 16
	)

	var buf bytes.Buffer

	// ICONDIR: reserved, type 1 (icon), image count.
	binary.Write(&buf, binary.LittleEndian, uint16(0))
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, uint16(len(variants)))

	offset := headerLen + entryLen*len(variants)
	for _, v := range variants {
		// ICONDIRENTRY. A 256px image is encoded as 0, since the field is a
		// single byte and 256 does not fit.
		dim := byte(v.size)
		if v.size >= 256 {
			dim = 0
		}
		buf.WriteByte(dim)                                           // width
		buf.WriteByte(dim)                                           // height
		buf.WriteByte(0)                                             // palette colours
		buf.WriteByte(0)                                             // reserved
		binary.Write(&buf, binary.LittleEndian, uint16(1))           // colour planes
		binary.Write(&buf, binary.LittleEndian, uint16(32))          // bits per pixel
		binary.Write(&buf, binary.LittleEndian, uint32(len(v.data))) // payload size
		binary.Write(&buf, binary.LittleEndian, uint32(offset))      // payload offset
		offset += len(v.data)
	}

	for _, v := range variants {
		buf.Write(v.data)
	}
	return buf.Bytes()
}

// Data returns the icon in the format the given platform's tray expects.
func Data(goos string) []byte {
	if goos == "windows" {
		return ICO()
	}
	return PNG()
}
