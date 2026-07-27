package icon

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"testing"
)

func TestEmbeddedVariantsDecodeAtTheirDeclaredSize(t *testing.T) {
	if len(variants) == 0 {
		t.Fatal("no icon variants embedded")
	}
	for _, v := range variants {
		if len(v.data) == 0 {
			t.Errorf("variant %d has no data; the go:embed directive did not resolve", v.size)
			continue
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(v.data))
		if err != nil {
			t.Errorf("variant %d is not decodable PNG: %v", v.size, err)
			continue
		}
		if cfg.Width != v.size || cfg.Height != v.size {
			t.Errorf("variant %d decodes as %dx%d", v.size, cfg.Width, cfg.Height)
		}
	}
}

func TestVariantsAreSortedAscending(t *testing.T) {
	// The ICO directory is written in slice order and Windows matches an entry
	// by requested size, so an unsorted list would hand back the wrong image.
	for i := 1; i < len(variants); i++ {
		if variants[i].size <= variants[i-1].size {
			t.Fatalf("variants are not ascending: %d follows %d", variants[i].size, variants[i-1].size)
		}
	}
}

// Parses the ICO the way Windows does and checks every offset resolves to a
// real image. A malformed container does not error at startup; it silently
// yields no tray icon, which is hard to notice and harder to diagnose.
func TestICOStructureIsValid(t *testing.T) {
	data := ICO()
	if len(data) < 6 {
		t.Fatal("ICO is too short to contain a header")
	}

	reserved := binary.LittleEndian.Uint16(data[0:2])
	kind := binary.LittleEndian.Uint16(data[2:4])
	count := int(binary.LittleEndian.Uint16(data[4:6]))

	if reserved != 0 {
		t.Errorf("reserved = %d, want 0", reserved)
	}
	if kind != 1 {
		t.Errorf("type = %d, want 1 (icon)", kind)
	}
	if count != len(variants) {
		t.Fatalf("image count = %d, want %d", count, len(variants))
	}

	for i := 0; i < count; i++ {
		base := 6 + i*16
		if base+16 > len(data) {
			t.Fatalf("directory entry %d runs past the end of the file", i)
		}
		entry := data[base : base+16]

		width, height := entry[0], entry[1]
		planes := binary.LittleEndian.Uint16(entry[4:6])
		bpp := binary.LittleEndian.Uint16(entry[6:8])
		size := binary.LittleEndian.Uint32(entry[8:12])
		offset := binary.LittleEndian.Uint32(entry[12:16])

		want := variants[i].size
		wantDim := byte(want)
		if want >= 256 {
			// 256 does not fit in a byte and is encoded as 0.
			wantDim = 0
		}
		if width != wantDim || height != wantDim {
			t.Errorf("entry %d: dimensions %dx%d, want %d", i, width, height, wantDim)
		}
		if planes != 1 {
			t.Errorf("entry %d: colour planes = %d, want 1", i, planes)
		}
		if bpp != 32 {
			t.Errorf("entry %d: bits per pixel = %d, want 32", i, bpp)
		}

		end := int(offset) + int(size)
		if int(offset) < 6+count*16 {
			t.Errorf("entry %d: payload offset %d overlaps the directory", i, offset)
		}
		if end > len(data) {
			t.Fatalf("entry %d: payload runs past the end (%d > %d)", i, end, len(data))
		}

		payload := data[offset:end]
		cfg, err := png.DecodeConfig(bytes.NewReader(payload))
		if err != nil {
			t.Errorf("entry %d: payload is not a valid PNG: %v", i, err)
			continue
		}
		if cfg.Width != want || cfg.Height != want {
			t.Errorf("entry %d: payload is %dx%d, want %dx%d", i, cfg.Width, cfg.Height, want, want)
		}
	}
}

// Every byte after the directory must belong to exactly one payload. A gap or
// an overlap means an offset was computed wrongly.
func TestICOPayloadsAreContiguous(t *testing.T) {
	data := ICO()
	count := int(binary.LittleEndian.Uint16(data[4:6]))

	expected := 6 + count*16
	for i := 0; i < count; i++ {
		entry := data[6+i*16 : 6+i*16+16]
		size := int(binary.LittleEndian.Uint32(entry[8:12]))
		offset := int(binary.LittleEndian.Uint32(entry[12:16]))
		if offset != expected {
			t.Errorf("entry %d: offset = %d, want %d (gap or overlap)", i, offset, expected)
		}
		expected += size
	}
	if expected != len(data) {
		t.Errorf("trailing bytes: payloads end at %d but the file is %d long", expected, len(data))
	}
}

func TestICODecodesAsAnImageEntry(t *testing.T) {
	// Sanity check that the largest payload really is renderable artwork, not
	// just structurally valid bytes.
	last := variants[len(variants)-1]
	img, err := png.Decode(bytes.NewReader(last.data))
	if err != nil {
		t.Fatalf("largest variant does not decode: %v", err)
	}
	if img.Bounds() != image.Rect(0, 0, last.size, last.size) {
		t.Errorf("bounds = %v, want %dx%d", img.Bounds(), last.size, last.size)
	}
}

func TestPNGSizeNeverScalesUp(t *testing.T) {
	cases := map[int]int{
		1:    16,  // smaller than anything embedded
		16:   16,  // exact
		17:   32,  // rounds up rather than reusing a smaller image
		64:   64,  // exact
		100:  128, // rounds up
		1000: 128, // larger than anything embedded, clamp to the biggest
	}
	for req, want := range cases {
		got := PNGSize(req)
		cfg, err := png.DecodeConfig(bytes.NewReader(got))
		if err != nil {
			t.Fatalf("PNGSize(%d) returned undecodable data: %v", req, err)
		}
		if cfg.Width != want {
			t.Errorf("PNGSize(%d) returned %dpx, want %dpx", req, cfg.Width, want)
		}
	}
}

func TestDataPerPlatform(t *testing.T) {
	win := Data("windows")
	if len(win) < 6 || binary.LittleEndian.Uint16(win[2:4]) != 1 {
		t.Error("windows should receive an ICO container")
	}
	for _, goos := range []string{"darwin", "linux"} {
		got := Data(goos)
		if _, err := png.DecodeConfig(bytes.NewReader(got)); err != nil {
			t.Errorf("%s should receive a PNG, got undecodable data: %v", goos, err)
		}
	}
}
