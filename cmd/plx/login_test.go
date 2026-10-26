package main

import (
	"regexp"
	"strings"
	"testing"

	"rsc.io/qr"
)

// qrBlocks must reproduce every module: ▀'s foreground is the top module and
// its background the bottom one, black (30/40) or white (97/107).
func TestQRBlocksRoundTrip(t *testing.T) {
	const text = "https://www.plex.tv/link/?pin=AB12"
	out, err := qrBlocks(text)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := qr.Encode(text, qr.M)
	cell := regexp.MustCompile(`\x1b\[(\d+);(\d+)m▀`)
	var grid [][]bool
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		var top, bot []bool
		for _, m := range cell.FindAllStringSubmatch(line, -1) {
			top = append(top, m[1] == "30")
			bot = append(bot, m[2] == "40")
		}
		grid = append(grid, top, bot)
	}
	const quiet = 2
	for y := range code.Size {
		for x := range code.Size {
			if grid[y+quiet][x+quiet] != code.Black(x, y) {
				t.Fatalf("module %d,%d differs", x, y)
			}
		}
	}
	for x := range grid[0] {
		if grid[0][x] || grid[1][x] {
			t.Fatal("quiet zone isn't white")
		}
	}
}
