//go:build ignore

// Icon generator. Everything magpie shows is drawn from one glyph: a small
// round magpie in profile, facing right, one outline on a 44-unit grid with the
// eye a hole. The app tile adds what a silhouette cannot: the white belly
// and the blue-green sheen on the tail. The same path lives in
// internal/gui/assets/index.html.
//
//	go run build/icon/gen.go tray internal/gui/tray.png     # 44px black template icon (macOS menu bar)
//	go run build/icon/gen.go app 64 internal/gui/icon.png   # coloured app icon at a given size
//	make icons                                              # regenerates all of them plus magpie.icns
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

// tray draws the silhouette as a macOS template image (black + alpha).
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
					if mark(fx, fy) != none {
						a++
					}
				}
			}
			img.SetNRGBA(x, y, color.NRGBA{0, 0, 0, uint8(a / 16 * 255)})
		}
	}
	return img
}

// app draws a rounded square, pale like paper, with the bird in ink, its
// belly white and its tail in the blue-green a magpie's tail really has.
func app(n int) image.Image {
	S := float64(n)
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	// macOS icon grid: the squircle occupies ~80% of the canvas
	inset := S * 0.1
	side := S - 2*inset
	radius := side * 0.225
	// the glyph's box is x 0.5..39.5, y 4.7..35.8; centre it on the tile
	k := side * 0.84 / 44
	ox, oy := inset+side/2-20*k, inset+side/2-20.25*k
	bg1 := [3]float64{0xfb, 0xfb, 0xfd} // top
	bg2 := [3]float64{0xe4, 0xe5, 0xec} // bottom
	black := [3]float64{0x17, 0x17, 0x1c}
	white := [3]float64{0xf3, 0xf3, 0xf7}
	sh1 := [3]float64{0x1e, 0x7f, 0xd0} // sheen at the tail base
	sh2 := [3]float64{0x24, 0xc3, 0xc4} // sheen at the tip
	ss := 4
	if n <= 64 {
		ss = 8
	}
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			var aBg float64
			var sum [3]float64 // colour of the glyph samples, premultiplied by count
			var aFg float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					fx := float64(x) + (float64(sx)+0.5)/float64(ss)
					fy := float64(y) + (float64(sy)+0.5)/float64(ss)
					if !roundRect(fx, fy, inset, inset, side, side, radius) {
						continue
					}
					aBg++
					u, v := (fx-ox)/k, (fy-oy)/k
					var c [3]float64
					switch mark(u, v) {
					case none:
						continue
					case ink:
						c = black
					case belly:
						c = white
					case sheen:
						// along the tail, base to tip
						t := math.Min(1, math.Max(0, (11.5-u)/11))
						for i := range c {
							c[i] = sh1[i]*(1-t) + sh2[i]*t
						}
					}
					aFg++
					for i := range c {
						sum[i] += c[i]
					}
				}
			}
			tot := float64(ss * ss)
			if aBg == 0 {
				img.SetNRGBA(x, y, color.NRGBA{})
				continue
			}
			t := float64(y) / S
			var px [3]float64
			for i := range px {
				bg := bg1[i]*(1-t) + bg2[i]*t
				px[i] = (bg*(aBg-aFg) + sum[i]) / aBg
			}
			img.SetNRGBA(x, y, color.NRGBA{uint8(px[0]), uint8(px[1]), uint8(px[2]), uint8(aBg / tot * 255)})
		}
	}
	return img
}

type region int

const (
	none region = iota
	ink
	belly
	sheen
)

// The bird, on its 44-unit grid, facing right: beak tip, crown, back,
// tail, belly, chest, throat.
var (
	outline            = flatten(`M39.5 16 L35 14.5 C33 7 24.5 4 18.5 7.5 C14.5 10 12 14.5 11.5 20 L0.5 27 L2.5 31 L12 27 C13.5 33 20 36.5 26 35 C31.5 33.5 34.5 29 35.5 24 C36 21 36 19 35.2 17.5 Z`)
	tail               = [][2]float64{{11.5, 20}, {0.5, 27}, {2.5, 31}, {12, 27}}
	eyeCX, eyeCY, eyeR = 31.0, 12.3, 2.3
	// the white belly: an oval kept inside the outline
	bellyCX, bellyCY, bellyRX, bellyRY, bellyRot = 24.0, 27.5, 8.5, 5.5, -12.0
)

// mark says what the glyph shows at a point: ink, the white belly, the
// tail's sheen, or nothing. Tray and header paint every region the same,
// so there the bird is a plain silhouette.
func mark(u, v float64) region {
	if math.Hypot(u-eyeCX, v-eyeCY) <= eyeR {
		return none
	}
	if !inPoly(u, v, outline) {
		return none
	}
	s, c := math.Sincos(bellyRot * math.Pi / 180)
	lx := (u-bellyCX)*c + (v-bellyCY)*s
	ly := -(u-bellyCX)*s + (v-bellyCY)*c
	if lx*lx/(bellyRX*bellyRX)+ly*ly/(bellyRY*bellyRY) <= 1 {
		return belly
	}
	if inPoly(u, v, tail) {
		return sheen
	}
	return ink
}

// flatten turns an SVG path of absolute M, L, C and Z commands (as in
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

// inPoly is even-odd point-in-polygon.
func inPoly(px, py float64, poly [][2]float64) bool {
	in := false
	n := len(poly)
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		xi, yi := poly[i][0], poly[i][1]
		xj, yj := poly[j][0], poly[j][1]
		if (yi > py) != (yj > py) && px < (xj-xi)*(py-yi)/(yj-yi)+xi {
			in = !in
		}
	}
	return in
}

// roundRect is a rounded square: true inside it.
func roundRect(px, py, x, y, w, h, r float64) bool {
	if px < x || py < y || px > x+w || py > y+h {
		return false
	}
	cx := math.Max(x+r, math.Min(px, x+w-r))
	cy := math.Max(y+r, math.Min(py, y+h-r))
	return math.Hypot(px-cx, py-cy) <= r
}
