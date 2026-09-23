//go:build ignore

// Icon generator. Everything dial shows is drawn from one glyph: a
// kingfisher in profile, facing right, big head and dagger beak, a short
// tail, the wing suggested by one thin line, the eye a hole. The same path
// lives in internal/gui/assets/index.html, on a 44-unit grid.
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
	"strings"
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

// app draws a rounded square, kingfisher blue, with the glyph in white.
func app(n int) image.Image {
	S := float64(n)
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	// macOS icon grid: the squircle occupies ~80% of the canvas
	inset := S * 0.1
	side := S - 2*inset
	radius := side * 0.225
	// the bird spans 42 of the 44 units across and sits a little high, so
	// it is drawn at 70% of the tile and nudged down to sit on centre
	k := side * 0.70 / 44
	ox, oy := inset+(side-44*k)/2, inset+(side-44*k)/2+1.5*k
	bg1 := [3]float64{0x22, 0xb8, 0xd4} // top: kingfisher teal
	bg2 := [3]float64{0x14, 0x67, 0xb4} // bottom: deeper blue
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

// mark is the glyph on its 44-unit grid. The outline is drawn facing left
// (beak at x=1) and mirrored, so the bird looks right, toward the name.
func mark(u, v float64) bool {
	u = 44 - u
	if !inside(u, v) {
		return false
	}
	if math.Hypot(u-18.4, v-13.3) <= 1.6 { // eye
		return false
	}
	if bezDist(u, v, wing) <= 0.7 { // wing line
		return false
	}
	return true
}

// outline is the body, beak to tail and back, as a flattened polygon.
var outline = flatten(`M1 15.5 L13.5 11.2 C15.5 6.8 21 4.2 26 5.6 C29.5 6.6 31 9.2 30.6 12.2 C34 14.8 37.5 19.5 39.2 25.5 L43.5 31.5 L39.2 34 C36 34.3 33.5 33.8 32 32.8 C28 37 19.5 36.5 15 29.5 C12.8 26 12.6 21.5 13.5 17.6 Z`)

var wing = [8]float64{27, 16.5, 31, 19.5, 35, 24.5, 37.5, 30.5}

// inside is even-odd point-in-polygon against the outline.
func inside(px, py float64) bool {
	in := false
	n := len(outline)
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		xi, yi := outline[i][0], outline[i][1]
		xj, yj := outline[j][0], outline[j][1]
		if (yi > py) != (yj > py) && px < (xj-xi)*(py-yi)/(yj-yi)+xi {
			in = !in
		}
	}
	return in
}

// flatten turns an SVG path of M, L, C and Z commands (absolute, as in
// index.html) into a polygon, each curve cut into short segments.
func flatten(d string) [][2]float64 {
	var pts [][2]float64
	f := strings.Fields(strings.NewReplacer("M", " M ", "L", " L ", "C", " C ", "Z", " Z ").Replace(d))
	num := func(i int) float64 { x, _ := strconv.ParseFloat(f[i], 64); return x }
	for i := 0; i < len(f); {
		switch f[i] {
		case "M", "L":
			pts = append(pts, [2]float64{num(i + 1), num(i + 2)})
			i += 3
		case "C":
			p := pts[len(pts)-1]
			c := [8]float64{p[0], p[1], num(i + 1), num(i + 2), num(i + 3), num(i + 4), num(i + 5), num(i + 6)}
			for k := 1; k <= 24; k++ {
				t := float64(k) / 24
				m := 1 - t
				pts = append(pts, [2]float64{
					m*m*m*c[0] + 3*m*m*t*c[2] + 3*m*t*t*c[4] + t*t*t*c[6],
					m*m*m*c[1] + 3*m*m*t*c[3] + 3*m*t*t*c[5] + t*t*t*c[7],
				})
			}
			i += 7
		default: // Z
			i++
		}
	}
	return pts
}

// bezDist is the distance from (px,py) to a cubic bezier, flattened into
// short segments; plenty for a stroke this wide.
func bezDist(px, py float64, c [8]float64) float64 {
	const n = 32
	best := math.Inf(1)
	x0, y0 := c[0], c[1]
	for i := 1; i <= n; i++ {
		t := float64(i) / n
		m := 1 - t
		x := m*m*m*c[0] + 3*m*m*t*c[2] + 3*m*t*t*c[4] + t*t*t*c[6]
		y := m*m*m*c[1] + 3*m*m*t*c[3] + 3*m*t*t*c[5] + t*t*t*c[7]
		best = math.Min(best, segDist(px, py, x0, y0, x, y))
		x0, y0 = x, y
	}
	return best
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
