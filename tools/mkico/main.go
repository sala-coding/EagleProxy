// Command mkico rewrites an icon file so that every frame is an uncompressed
// bitmap rather than a PNG.
//
// Why it exists: the tray reads its icon with LoadImage, which understands a
// PNG inside an icon file, and the artwork stays small that way. The window's
// icon does not: wails compiles the icon into the executable as a resource, and
// the window framework asks for it with LoadIcon, which reads only the
// uncompressed form. A PNG frame read that way comes out as a black square —
// which is what the taskbar button and the title bar were showing.
//
// The frames themselves are not touched: the same picture, the same sizes, the
// same order. Only the encoding changes.
//
// Usage:
//
//	go run ./tools/mkico <source.ico> <target.ico>
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: mkico <source.ico> <target.ico>")
		os.Exit(2)
	}
	frames, err := readFrames(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "read:", err)
		os.Exit(1)
	}
	out, err := writeICO(frames)
	if err != nil {
		fmt.Fprintln(os.Stderr, "write:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Args[2], out, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write:", err)
		os.Exit(1)
	}
	fmt.Printf("%s: %d frame(s) -> %s (%d bytes)\n", os.Args[1], len(frames), os.Args[2], len(out))
}

// frame is one picture of an icon, with the size the icon file said it was.
type frame struct {
	img   image.Image
	width int
}

// readFrames reads every picture out of an icon file. The frames may be PNG
// (which is what the artwork is written as) or uncompressed bitmaps; both are
// decoded here and handed on as pictures.
func readFrames(path string) ([]frame, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 6 || binary.LittleEndian.Uint16(data[0:2]) != 0 || binary.LittleEndian.Uint16(data[2:4]) != 1 {
		return nil, fmt.Errorf("not an icon file")
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	frames := make([]frame, 0, count)
	for i := 0; i < count; i++ {
		e := 6 + 16*i
		if e+16 > len(data) {
			return nil, fmt.Errorf("frame %d: the directory is cut short", i)
		}
		width := int(data[e])
		if width == 0 {
			width = 256
		}
		size := int(binary.LittleEndian.Uint32(data[e+8 : e+12]))
		offset := int(binary.LittleEndian.Uint32(data[e+12 : e+16]))
		if offset+size > len(data) {
			return nil, fmt.Errorf("frame %d: the picture is cut short", i)
		}
		img, err := decode(data[offset : offset+size])
		if err != nil {
			return nil, fmt.Errorf("frame %d: %w", i, err)
		}
		frames = append(frames, frame{img: img, width: width})
	}
	return frames, nil
}

// decode reads one picture: a PNG, or a bitmap with its header.
func decode(data []byte) (image.Image, error) {
	if len(data) > 8 && data[0] == 0x89 && data[1] == 'P' {
		return png.Decode(bytes.NewReader(data))
	}
	return decodeBitmap(data)
}

// decodeBitmap reads a frame that is already an uncompressed bitmap — the form
// this tool writes, and the form some tools write directly.
func decodeBitmap(data []byte) (image.Image, error) {
	if len(data) < 40 {
		return nil, fmt.Errorf("the picture is too short")
	}
	headerSize := binary.LittleEndian.Uint32(data[0:4])
	if headerSize < 40 {
		return nil, fmt.Errorf("unexpected bitmap header")
	}
	w := int(int32(binary.LittleEndian.Uint32(data[4:8])))
	h := int(int32(binary.LittleEndian.Uint32(data[8:12])))
	if w <= 0 {
		return nil, fmt.Errorf("the picture has no width")
	}
	// In an icon the height counts the picture and the mask together.
	if h%2 != 0 {
		return nil, fmt.Errorf("unexpected bitmap height")
	}
	h /= 2
	bits := binary.LittleEndian.Uint16(data[14:16])
	if bits != 32 {
		return nil, fmt.Errorf("only 32-bit pictures are read (%d bits here)", bits)
	}
	pixels := data[headerSize:]
	if len(pixels) < w*h*4 {
		return nil, fmt.Errorf("the picture is cut short")
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		// The picture is stored bottom-up.
		src := (h - 1 - y) * w * 4
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: pixels[src+x*4+2],
				G: pixels[src+x*4+1],
				B: pixels[src+x*4+0],
				A: pixels[src+x*4+3],
			})
		}
	}
	return img, nil
}

// writeICO packs the pictures into an icon file whose frames are uncompressed
// bitmaps: a header, a directory, and then the pictures themselves.
func writeICO(frames []frame) ([]byte, error) {
	// The picture of each frame first, so the directory can say how long it is.
	pictures := make([][]byte, len(frames))
	for i, f := range frames {
		p, err := encodeBitmap(f.img)
		if err != nil {
			return nil, fmt.Errorf("frame %d: %w", i, err)
		}
		pictures[i] = p
	}
	dirSize := 6 + 16*len(frames)
	out := &bytes.Buffer{}
	binary.Write(out, binary.LittleEndian, uint16(0)) // reserved
	binary.Write(out, binary.LittleEndian, uint16(1)) // type: icon
	binary.Write(out, binary.LittleEndian, uint16(len(frames)))
	offset := dirSize
	for i, f := range frames {
		b := f.img.Bounds()
		w, h := b.Dx(), b.Dy()
		entry := make([]byte, 16)
		entry[0] = byte(w % 256) // 0 means 256
		entry[1] = byte(h % 256)
		entry[2] = 0 // colours in the palette: none, the picture is 32-bit
		entry[3] = 0
		binary.LittleEndian.PutUint16(entry[4:6], 1)  // planes
		binary.LittleEndian.PutUint16(entry[6:8], 32) // bits per pixel
		binary.LittleEndian.PutUint32(entry[8:12], uint32(len(pictures[i])))
		binary.LittleEndian.PutUint32(entry[12:16], uint32(offset))
		out.Write(entry)
		offset += len(pictures[i])
	}
	for _, p := range pictures {
		out.Write(p)
	}
	return out.Bytes(), nil
}

// encodeBitmap writes a picture the way an icon file wants it: a bitmap header,
// the picture itself bottom-up in blue-green-red-alpha, and then the mask that
// says which of those pixels are transparent. The alpha channel already says
// it, so the mask is empty.
func encodeBitmap(img image.Image) ([]byte, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return nil, fmt.Errorf("the picture is empty")
	}
	pixels := make([]byte, 0, w*h*4)
	for y := h - 1; y >= 0; y-- { // bottom-up
		for x := 0; x < w; x++ {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			pixels = append(pixels, byte(bl>>8), byte(g>>8), byte(r>>8), byte(a>>8))
		}
	}
	maskRow := ((w + 31) / 32) * 4
	mask := make([]byte, maskRow*h)

	out := &bytes.Buffer{}
	header := make([]byte, 40)
	binary.LittleEndian.PutUint32(header[0:4], 40) // the size of this header
	binary.LittleEndian.PutUint32(header[4:8], uint32(w))
	binary.LittleEndian.PutUint32(header[8:12], uint32(h*2)) // the picture and the mask
	binary.LittleEndian.PutUint16(header[12:14], 1)          // planes
	binary.LittleEndian.PutUint16(header[14:16], 32)         // bits per pixel
	binary.LittleEndian.PutUint32(header[16:20], 0)          // BI_RGB
	binary.LittleEndian.PutUint32(header[20:24], uint32(len(pixels)))
	out.Write(header)
	out.Write(pixels)
	out.Write(mask)
	return out.Bytes(), nil
}
