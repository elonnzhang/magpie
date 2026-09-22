//go:build ignore

// Icon generator. Everything dial shows is drawn from one glyph, "detent":
// a control ring open to the upper right with a detached index reaching
// through the opening, the instant a knob clicks into position. The same
// paths live in internal/gui/assets/index.html, on a 44-unit grid.
//
//	go run build/icon/gen.go tray internal/gui/tray.png     # 44px black template icon (macOS menu bar)
//	go run build/icon/gen.go app 64 internal/gui/icon.png   # coloured app icon at a given size
//	make icons                                              # regenerates all of them plus dial.icns
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"strconv"
)

func main() {
	var img image.Image
	switch {
	case len(os.Args) == 3 && os.Args[1] == "tray":
		img = tray()
	case len(os.Args) == 4 && os.Args[1] == "app":
		n, _ := strconv.Atoi(os.Args[2])
		img = app(n)
	default:
		fmt.Fprintln(os.Stderr, "usage: gen.go tray <out.png> | gen.go app <size> <out.png>")
		os.Exit(2)
	}
	f, err := os.Create(os.Args[len(os.Args)-1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// tray draws the glyph as a macOS template image (black + alpha).
func tray() image.Image {
	const S = 44
	img := image.NewNRGBA(image.Rect(0, 0, S, S))
	for y := 0; y < S; y++ {
		for x := 0; x < S; x++ {
			a := 0.0
			// supersample 4x4
			for sy := 0; sy < 4; sy++ {
				for sx := 0; sx < 4; sx++ {
					fx := float64(x) + (float64(sx)+0.5)/4
					fy := float64(y) + (float64(sy)+0.5)/4
					if mark(fx, fy) {
						a++
					}
				}
			}
			img.SetNRGBA(x, y, color.NRGBA{0, 0, 0, uint8(a / 16 * 255)})
		}
	}
	return img
}

// app draws a rounded indigo square with the glyph in white.
func app(n int) image.Image {
	S := float64(n)
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	// macOS icon grid: the squircle occupies ~80% of the canvas
	inset := S * 0.1
	side := S - 2*inset
	radius := side * 0.225
	// the mark fills 75% of the tile, as in the reference SVG (scale 14 on 824)
	k := side * 0.747 / 44
	ox, oy := inset+(side-44*k)/2, inset+(side-44*k)/2
	bg1 := [3]float64{0x5b, 0x55, 0xf0} // top: lighter indigo
	bg2 := [3]float64{0x3f, 0x37, 0xc9} // bottom: deeper
	ss := 4
	if n <= 64 {
		ss = 8
	}
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			var aBg, aFg float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					fx := float64(x) + (float64(sx)+0.5)/float64(ss)
					fy := float64(y) + (float64(sy)+0.5)/float64(ss)
					if roundRect(fx, fy, inset, inset, side, side, radius) {
						aBg++
						if mark((fx-ox)/k, (fy-oy)/k) {
							aFg++
						}
					}
				}
			}
			tot := float64(ss * ss)
			t := float64(y) / S
			r := bg1[0]*(1-t) + bg2[0]*t
			g := bg1[1]*(1-t) + bg2[1]*t
			b := bg1[2]*(1-t) + bg2[2]*t
			fg := aFg / tot
			if aBg > 0 {
				fg /= aBg / tot
			}
			img.SetNRGBA(x, y, color.NRGBA{
				uint8(r*(1-fg) + 255*fg), uint8(g*(1-fg) + 255*fg), uint8(b*(1-fg) + 255*fg),
				uint8(aBg / tot * 255),
			})
		}
	}
	return img
}

// mark is the glyph on its 44-unit grid: a 4.5-wide ring of radius 15 about
// (22,22), open between -75° and -15° (upper right), plus a round-capped
// index from (25,19) to (34,10). Both strokes have round caps.
func mark(u, v float64) bool {
	const cx, cy, r, w = 22.0, 22.0, 15.0, 4.5
	dx, dy := u-cx, v-cy
	d := math.Hypot(dx, dy)
	ang := math.Atan2(dy, dx) * 180 / math.Pi // y down: -90 is up
	if math.Abs(d-r) <= w/2 && !(ang > -75 && ang < -15) {
		return true
	}
	// round caps at the two ends of the opening
	for _, a := range []float64{-75, -15} {
		ex, ey := cx+r*math.Cos(a*math.Pi/180), cy+r*math.Sin(a*math.Pi/180)
		if math.Hypot(u-ex, v-ey) <= w/2 {
			return true
		}
	}
	return segDist(u, v, 25, 19, 34, 10) <= w/2
}

func roundRect(px, py, x, y, w, h, r float64) bool {
	if px < x || py < y || px > x+w || py > y+h {
		return false
	}
	qx := math.Max(math.Abs(px-(x+w/2))-(w/2-r), 0)
	qy := math.Max(math.Abs(py-(y+h/2))-(h/2-r), 0)
	return math.Hypot(qx, qy) <= r
}

func segDist(px, py, ax, ay, bx, by float64) float64 {
	vx, vy := bx-ax, by-ay
	wx, wy := px-ax, py-ay
	t := math.Max(0, math.Min(1, (wx*vx+wy*vy)/(vx*vx+vy*vy)))
	return math.Hypot(px-(ax+t*vx), py-(ay+t*vy))
}
