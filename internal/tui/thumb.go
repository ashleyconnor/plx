package tui

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // Plex artwork
	_ "image/png"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// halfBlocks renders an image as text: each cell is "▀" with the upper pixel
// as foreground and the lower as background, so a cols×rows box shows
// cols×2·rows pixels. It works in any terminal with 256 or true colour
// (lipgloss downsamples), and needs no graphics protocol.
func halfBlocks(data []byte, cols, rows int) (string, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	b := img.Bounds()
	if b.Dx() == 0 || b.Dy() == 0 {
		return "", fmt.Errorf("empty image")
	}
	// Fit the image inside cols × 2·rows pixels, keeping its aspect ratio.
	pw, ph := cols, cols*b.Dy()/b.Dx()
	if ph > 2*rows {
		ph = 2 * rows
		pw = max(1, ph*b.Dx()/b.Dy())
	}
	ph -= ph % 2
	pad := strings.Repeat(" ", (cols-pw)/2)
	var out strings.Builder
	for y := 0; y < ph; y += 2 {
		out.WriteString(pad)
		for x := 0; x < pw; x++ {
			top := average(img, b, x, y, pw, ph)
			bot := average(img, b, x, y+1, pw, ph)
			out.WriteString(lipgloss.NewStyle().Foreground(top).Background(bot).Render("▀"))
		}
		out.WriteString(strings.Repeat(" ", cols-pw-len(pad)))
		if y+2 < ph {
			out.WriteByte('\n')
		}
	}
	return out.String(), nil
}

// average box-filters the source pixels that map onto target pixel (x, y).
func average(img image.Image, b image.Rectangle, x, y, pw, ph int) lipgloss.Color {
	x0 := b.Min.X + x*b.Dx()/pw
	x1 := max(x0+1, b.Min.X+(x+1)*b.Dx()/pw)
	y0 := b.Min.Y + y*b.Dy()/ph
	y1 := max(y0+1, b.Min.Y+(y+1)*b.Dy()/ph)
	var r, g, bl, n uint64
	for sy := y0; sy < y1; sy++ {
		for sx := x0; sx < x1; sx++ {
			cr, cg, cb, _ := img.At(sx, sy).RGBA()
			r, g, bl, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), n+1
		}
	}
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r/n>>8, g/n>>8, bl/n>>8))
}
