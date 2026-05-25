// genicon writes icon.ico in the repo root: a multi-resolution Windows
// icon built from a 16x16 pixel-art square wave on a NES-blue background,
// nearest-neighbor scaled to 16/32/48/64/128/256. Run from repo root:
//
//	go run ./tools/genicon
//
// Then convert to a linkable resource:
//
//	go run github.com/akavel/rsrc -ico icon.ico -o rsrc_windows_amd64.syso
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
)

var (
	bgBlue     = color.RGBA{0x1A, 0x2C, 0x56, 0xFF}
	waveYellow = color.RGBA{0xF9, 0xE8, 0x4B, 0xFF}
)

// 16x16 source: two complete square-wave pulses centred vertically, with
// low extensions on either side. Hand-pixeled so it reads cleanly at
// taskbar size, then nearest-neighbor-scaled for the larger sizes to keep
// the chunky pixel-art look.
var iconBits = []string{
	"................",
	"................",
	"................",
	"................",
	"..#####...#####.",
	"..#...#...#...#.",
	"..#...#...#...#.",
	"..#...#...#...#.",
	"..#...#...#...#.",
	"..#...#...#...#.",
	"..#...#...#...#.",
	"###...#####...##",
	"................",
	"................",
	"................",
	"................",
}

func renderIcon(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	scale := size / 16
	if scale < 1 {
		scale = 1
	}
	for sy, row := range iconBits {
		for sx, ch := range row {
			c := color.Color(bgBlue)
			if ch == '#' {
				c = waveYellow
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					px, py := sx*scale+dx, sy*scale+dy
					if px < size && py < size {
						img.Set(px, py, c)
					}
				}
			}
		}
	}
	return img
}

func writeICO(path string, sizes []int) error {
	type entry struct {
		size int
		data []byte
	}
	var entries []entry
	for _, sz := range sizes {
		var buf bytes.Buffer
		if err := png.Encode(&buf, renderIcon(sz)); err != nil {
			return err
		}
		entries = append(entries, entry{sz, buf.Bytes()})
	}

	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, uint16(0))
	binary.Write(&out, binary.LittleEndian, uint16(1))
	binary.Write(&out, binary.LittleEndian, uint16(len(entries)))

	offset := 6 + 16*len(entries)
	for _, e := range entries {
		// 256 is encoded as 0 in the byte field.
		w, h := byte(e.size), byte(e.size)
		if e.size >= 256 {
			w, h = 0, 0
		}
		out.WriteByte(w)
		out.WriteByte(h)
		out.WriteByte(0)
		out.WriteByte(0)
		binary.Write(&out, binary.LittleEndian, uint16(1))
		binary.Write(&out, binary.LittleEndian, uint16(32))
		binary.Write(&out, binary.LittleEndian, uint32(len(e.data)))
		binary.Write(&out, binary.LittleEndian, uint32(offset))
		offset += len(e.data)
	}
	for _, e := range entries {
		out.Write(e.data)
	}
	return os.WriteFile(path, out.Bytes(), 0o644)
}

func main() {
	sizes := []int{16, 32, 48, 64, 128, 256}
	if err := writeICO("icon.ico", sizes); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote icon.ico with sizes %v\n", sizes)
}
