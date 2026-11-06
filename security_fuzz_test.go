package ntpdf

import (
	"image"
	"image/color"
	"math"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
	"nimbleterminal.dev/ntmediabox"
)

// Build valid envelopes so mutations reach the encoder and typed decoder,
// complementing the arbitrary JSON in FuzzDecodeWorkerPages.
func FuzzWorkerPagesRoundTrip(f *testing.F) {
	f.Add("hello", "Helvetica", uint16(612), uint16(792), uint8(1), uint8(0))
	f.Add("\x1b[2J\u202e", "", uint16(1), uint16(1), uint8(3), uint8(4))
	f.Fuzz(func(t *testing.T, text, font string, width, height uint16, count, images uint8) {
		if len(text) > 4096 || len(font) > 4096 || !utf8.ValidString(text) || !utf8.ValidString(font) {
			t.Skip()
		}
		pages := make([]pdfPage, 1+int(count)%8)
		for i := range pages {
			pages[i] = pdfPage{
				media:      mediaBox{0, 0, float64(width) + 1, float64(height) + 1},
				runs:       []pdf.Text{{X: float64(i), Y: 12, W: 20, FontSize: 12, Font: font, S: text}},
				imageCount: int(images),
			}
		}
		limits := Limits{MaxPages: len(pages), MaxImagesPerPage: int(images)}
		data, err := encodeWorkerPages(pages, limits)
		if err != nil {
			t.Fatal(err)
		}
		got, err := decodeWorkerPages(data, limits)
		if err != nil || !reflect.DeepEqual(got, pages) {
			t.Fatalf("metadata round trip: %v", err)
		}
		if len(pages) > 1 {
			limits.MaxPages--
			if _, err := decodeWorkerPages(data, limits); err == nil {
				t.Fatal("accepted metadata over the page limit")
			}
		}
	})
}

func FuzzSanitizeForTerminal(f *testing.F) {
	for _, s := range []string{"plain ✓", "\x1b]52;c;clipboard\x07\x1b[2J\u202e\u2066", "\xff\x80\x00\r\n\t", "a\u2028b\u200bc"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 64<<10 {
			t.Skip()
		}
		out := SanitizeForTerminal(s)
		if !utf8.ValidString(out) {
			t.Fatal("sanitizer returned invalid UTF-8")
		}
		for _, r := range out {
			if !unicode.IsPrint(r) {
				t.Fatalf("unsafe output rune %U", r)
			}
		}
		if SanitizeForTerminal(out) != out {
			t.Fatal("sanitizer is not idempotent")
		}
	})
}

func FuzzLayoutRuns(f *testing.F) {
	f.Add("safe text", 50.0, 700.0, 12.0, uint8(80), uint8(24))
	f.Add("\x1b[31m\u202e\xff", math.Inf(1), math.NaN(), math.MaxFloat64, uint8(0), uint8(1))
	f.Add("overflow", -math.MaxFloat64, math.SmallestNonzeroFloat64, math.Inf(-1), uint8(17), uint8(9))
	f.Fuzz(func(t *testing.T, s string, x, y, extent float64, columns, lines uint8) {
		if len(s) > 4096 {
			t.Skip()
		}
		cols, rows := int(columns)%81, int(lines)%25
		runs := []pdf.Text{
			{X: 50, Y: 700, W: 60, FontSize: 12, S: s},
			{X: x, Y: y, W: extent, FontSize: extent, S: s},
			{X: -x, Y: -y, W: -extent, FontSize: 12, S: s},
		}
		// Exercise normal, degenerate, and hostile geometry with a bounded host
		// viewport. Document coordinates must never dictate grid allocation.
		for _, box := range []mediaBox{defaultMediaBox(), {x, y, x + extent, y + extent}, {0, 0, 0, 0}} {
			grid := layoutRuns(runs, box, cols, rows)
			if len(grid) != rows {
				t.Fatalf("got %d rows, want %d", len(grid), rows)
			}
			for _, row := range grid {
				if len(row) != cols {
					t.Fatalf("got %d columns, want %d", len(row), cols)
				}
				for _, r := range row {
					if !unicode.IsPrint(r) {
						t.Fatalf("unsafe grid rune %U", r)
					}
				}
			}
			if out := joinGrid(grid); !utf8.ValidString(out) || strings.Count(out, "\n") != max(0, rows-1) {
				t.Fatal("invalid grid serialization")
			}
		}
	})
}

type fuzzBudgetImage struct {
	rect  image.Rectangle
	reads int
}

func (p *fuzzBudgetImage) Bounds() image.Rectangle { return p.rect }
func (p *fuzzBudgetImage) ColorModel() color.Model { return color.RGBAModel }
func (p *fuzzBudgetImage) At(int, int) color.Color { p.reads++; return color.RGBA{R: 7, A: 255} }

func FuzzCheckedWorkerImage(f *testing.F) {
	for _, v := range [][9]int{
		{0, 0, 2, 2, 0, 0, 4, 4, 0}, {0, 0, 1, 1, 0, 0, 0, 0, 1}, {9, -3, 11, -1, 2, 2, 4, 4, 0},
		{0, 0, 3, 3, 0, 0, 4, -1, 0}, {0, 0, 8193, 1, 0, 0, 0, 0, 0},
		{math.MinInt, 0, math.MaxInt, 1, 0, 0, 0, 0, 0},
		{math.MaxInt, 0, math.MinInt, 1, 0, 0, 0, 0, 0},
		{0, 0, 1, 1, math.MinInt, math.MaxInt, math.MaxInt, -1, 0},
	} {
		f.Add(v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7], v[8])
	}
	f.Fuzz(func(t *testing.T, x0, y0, x1, y1, maxW, maxH, requestPixels, sessionPixels, nilImage int) {
		req := ntmediabox.RenderRequest{Width: maxW, Height: maxH, MaxPixels: requestPixels}
		lim := Limits{MaxRenderPixels: sessionPixels}
		if nilImage&1 != 0 {
			if _, err := checkedWorkerImage(nil, req, lim); err == nil {
				t.Fatal("accepted nil image")
			}
			return
		}
		// Use arbitrary-precision subtraction/product as an independent oracle;
		// image.Rectangle.Dx/Dy can overflow before the implementation checks them.
		w := new(big.Int).Sub(big.NewInt(int64(x1)), big.NewInt(int64(x0)))
		h := new(big.Int).Sub(big.NewInt(int64(y1)), big.NewInt(int64(y0)))
		widthLimit, heightLimit, pixelLimit := ntmediabox.MaxRenderSide, ntmediabox.MaxRenderSide, ntmediabox.MaxRenderPixels
		if maxW > 0 {
			widthLimit = min(widthLimit, maxW)
		}
		if maxH > 0 {
			heightLimit = min(heightLimit, maxH)
		}
		if requestPixels > 0 {
			pixelLimit = min(pixelLimit, requestPixels)
		}
		if sessionPixels > 0 {
			pixelLimit = min(pixelLimit, sessionPixels)
		}
		pixels := new(big.Int).Mul(w, h)
		valid := w.Sign() > 0 && h.Sign() > 0 && w.Cmp(big.NewInt(int64(widthLimit))) <= 0 && h.Cmp(big.NewInt(int64(heightLimit))) <= 0 && pixels.Cmp(big.NewInt(int64(pixelLimit))) <= 0
		// Successful conversion is intentionally tiny; huge rejected bounds still
		// run through the check without allocating or touching source pixels.
		if valid && pixels.Cmp(big.NewInt(4096)) > 0 {
			t.Skip()
		}
		src := &fuzzBudgetImage{rect: image.Rectangle{Min: image.Pt(x0, y0), Max: image.Pt(x1, y1)}}
		out, err := checkedWorkerImage(src, req, lim)
		if (err == nil) != valid {
			t.Fatalf("bounds %v: accepted=%v, want %v", src.rect, err == nil, valid)
		}
		if !valid {
			if src.reads != 0 {
				t.Fatal("converted rejected image")
			}
			return
		}
		if out == nil || out.Bounds() != image.Rect(0, 0, int(w.Int64()), int(h.Int64())) || out.Stride != int(w.Int64())*4 {
			t.Fatal("invalid converted image dimensions")
		}
		if out.RGBAAt(0, 0) != (color.RGBA{R: 7, A: 255}) {
			t.Fatal("conversion corrupted pixels")
		}
	})
}
