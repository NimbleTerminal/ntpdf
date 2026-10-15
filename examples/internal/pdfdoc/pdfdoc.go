// Package pdfdoc builds deterministic, standards-compliant PDF 1.4 documents
// in pure Go for the ntpdf examples and interactive browser demos.
//
// Every generated document includes positioned Type1 text (/Helvetica,
// /Helvetica-Bold, /Courier with /WinAnsiEncoding) and vector graphics paths
// so both ntpdf.ImageMode (PDFium rasterization) and ntpdf.TextMode
// (ledongthuc/pdf positioned text extraction) produce rich output.
package pdfdoc

import (
	"bytes"
	"fmt"
	"math"
	"strings"
)

// RGB is an [0, 1] RGB color triple used by PDF vector and text operators.
type RGB struct{ R, G, B float64 }

// Hex parses a "#rrggbb" hex color into an RGB triple.
func Hex(s string) RGB {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return RGB{0, 0, 0}
	}
	var r, g, b uint8
	_, _ = fmt.Sscanf(s, "%02x%02x%02x", &r, &g, &b)
	return RGB{float64(r) / 255.0, float64(g) / 255.0, float64(b) / 255.0}
}

// Font identifies one of the three built-in PDF Type1 fonts registered on
// every page resource dictionary.
type Font string

const (
	FontSans     Font = "F1" // Helvetica
	FontSansBold Font = "F2" // Helvetica-Bold
	FontMono     Font = "F3" // Courier
)

// Page accumulates PDF content-stream operations for a single page.
type Page struct {
	Width, Height float64
	buf           bytes.Buffer
	hasBadgeImage bool
}

// Builder constructs a multi-page PDF 1.4 document in memory.
type Builder struct {
	pages []*Page
}

// New creates an empty PDF document builder.
func New() *Builder {
	return &Builder{}
}

// AddPage appends a page with the given dimensions in PDF points (1/72 inch).
func (b *Builder) AddPage(width, height float64) *Page {
	p := &Page{Width: width, Height: height}
	b.pages = append(b.pages, p)
	return p
}

// Background fills the entire page rectangle with c.
func (p *Page) Background(c RGB) {
	p.RectFill(0, 0, p.Width, p.Height, c)
}

// RectFill draws a filled axis-aligned rectangle.
func (p *Page) RectFill(x, y, w, h float64, fill RGB) {
	fmt.Fprintf(&p.buf, "q %.3f %.3f %.3f rg %.2f %.2f %.2f %.2f re f Q\n",
		fill.R, fill.G, fill.B, x, y, w, h)
}

// RectStroke draws a stroked axis-aligned rectangle.
func (p *Page) RectStroke(x, y, w, h, lineW float64, stroke RGB) {
	fmt.Fprintf(&p.buf, "q %.2f w %.3f %.3f %.3f RG %.2f %.2f %.2f %.2f re S Q\n",
		lineW, stroke.R, stroke.G, stroke.B, x, y, w, h)
}

// RectFillStroke draws a filled and stroked rectangle.
func (p *Page) RectFillStroke(x, y, w, h, lineW float64, fill, stroke RGB) {
	fmt.Fprintf(&p.buf, "q %.2f w %.3f %.3f %.3f rg %.3f %.3f %.3f RG %.2f %.2f %.2f %.2f re B Q\n",
		lineW, fill.R, fill.G, fill.B, stroke.R, stroke.G, stroke.B, x, y, w, h)
}

// Line draws a line segment from (x1, y1) to (x2, y2).
func (p *Page) Line(x1, y1, x2, y2, lineW float64, stroke RGB) {
	fmt.Fprintf(&p.buf, "q %.2f w 1 J %.3f %.3f %.3f RG %.2f %.2f m %.2f %.2f l S Q\n",
		lineW, stroke.R, stroke.G, stroke.B, x1, y1, x2, y2)
}

// DashedLine draws a dashed line segment from (x1, y1) to (x2, y2).
func (p *Page) DashedLine(x1, y1, x2, y2, lineW float64, stroke RGB) {
	fmt.Fprintf(&p.buf, "q %.2f w [4 4] 0 d %.3f %.3f %.3f RG %.2f %.2f m %.2f %.2f l S Q\n",
		lineW, stroke.R, stroke.G, stroke.B, x1, y1, x2, y2)
}

// Polyline strokes a connected series of points.
func (p *Page) Polyline(pts [][2]float64, lineW float64, stroke RGB) {
	if len(pts) < 2 {
		return
	}
	fmt.Fprintf(&p.buf, "q %.2f w 1 J 1 j %.3f %.3f %.3f RG %.2f %.2f m",
		lineW, stroke.R, stroke.G, stroke.B, pts[0][0], pts[0][1])
	for _, pt := range pts[1:] {
		fmt.Fprintf(&p.buf, " %.2f %.2f l", pt[0], pt[1])
	}
	p.buf.WriteString(" S Q\n")
}

// CircleFill draws a filled circle centered at (cx, cy) with radius r using
// a 4-arc cubic Bezier approximation.
func (p *Page) CircleFill(cx, cy, r float64, fill RGB) {
	const k = 0.5522847498
	kr := k * r
	fmt.Fprintf(&p.buf, "q %.3f %.3f %.3f rg ", fill.R, fill.G, fill.B)
	fmt.Fprintf(&p.buf, "%.2f %.2f m ", cx+r, cy)
	fmt.Fprintf(&p.buf, "%.2f %.2f %.2f %.2f %.2f %.2f c ", cx+r, cy+kr, cx+kr, cy+r, cx, cy+r)
	fmt.Fprintf(&p.buf, "%.2f %.2f %.2f %.2f %.2f %.2f c ", cx-kr, cy+r, cx-r, cy+kr, cx-r, cy)
	fmt.Fprintf(&p.buf, "%.2f %.2f %.2f %.2f %.2f %.2f c ", cx-r, cy-kr, cx-kr, cy-r, cx, cy-r)
	fmt.Fprintf(&p.buf, "%.2f %.2f %.2f %.2f %.2f %.2f c f Q\n", cx+kr, cy-r, cx+r, cy-kr, cx+r, cy)
}

// DrawBadgeImage places an embedded 8x8 RGB Image XObject at (x, y, w, h) so
// both PDFium rasterization and TextMode image-placeholder detection see an
// embedded /Subtype /Image resource on the page.
func (p *Page) DrawBadgeImage(x, y, w, h float64) {
	p.hasBadgeImage = true
	fmt.Fprintf(&p.buf, "q %.2f 0 0 %.2f %.2f %.2f cm /Im1 Do Q\n", w, h, x, y)
}

// Text draws a single-line ASCII text string at (x, y) in PDF points.
func (p *Page) Text(x, y float64, font Font, size float64, color RGB, text string) {
	if font == "" {
		font = FontSans
	}
	fmt.Fprintf(&p.buf, "BT /%s %.2f Tf %.3f %.3f %.3f rg 1 0 0 1 %.2f %.2f Tm (%s) Tj ET\n",
		font, size, color.R, color.G, color.B, x, y, escapePDFString(text))
}

func escapePDFString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '(':
			b.WriteString(`\(`)
		case ')':
			b.WriteString(`\)`)
		default:
			if r >= 32 && r <= 126 {
				b.WriteRune(r)
			} else {
				b.WriteByte(' ')
			}
		}
	}
	return b.String()
}

// badgeImageRaw is an 8x8 RGB gradient icon (192 bytes) used when a page calls
// DrawBadgeImage.
func badgeImageRaw() []byte {
	raw := make([]byte, 8*8*3)
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			i := (y*8 + x) * 3
			raw[i+0] = uint8(40 + x*24)
			raw[i+1] = uint8(140 + y*14)
			raw[i+2] = uint8(220 - x*12)
		}
	}
	return raw
}

// Bytes serializes the PDF 1.4 document with an exact cross-reference table.
func (b *Builder) Bytes() []byte {
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")

	// Object numbering layout:
	// 1: Catalog
	// 2: Pages
	// 3: Font F1 (Helvetica)
	// 4: Font F2 (Helvetica-Bold)
	// 5: Font F3 (Courier)
	// 6: Image XObject Im1 (8x8 RGB icon)
	// For each page i (0..N-1):
	//   pageObj = 7 + i*2
	//   contentObj = 8 + i*2
	totalObjs := 6 + len(b.pages)*2
	offsets := make([]int, totalObjs+1)

	writeObj := func(id int, body string) {
		offsets[id] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", id, body)
	}

	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")

	var kids strings.Builder
	for i := range b.pages {
		if i > 0 {
			kids.WriteByte(' ')
		}
		fmt.Fprintf(&kids, "%d 0 R", 7+i*2)
	}
	writeObj(2, fmt.Sprintf("<< /Type /Pages /Count %d /Kids [ %s ] >>", len(b.pages), kids.String()))

	writeObj(3, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
	writeObj(4, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>")
	writeObj(5, "<< /Type /Font /Subtype /Type1 /BaseFont /Courier /Encoding /WinAnsiEncoding >>")

	imgData := badgeImageRaw()
	offsets[6] = out.Len()
	fmt.Fprintf(&out, "6 0 obj\n<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Length %d >>\nstream\n", len(imgData))
	out.Write(imgData)
	out.WriteString("\nendstream\nendobj\n")

	for i, pg := range b.pages {
		pageID := 7 + i*2
		contentID := 8 + i*2
		xobjRes := ""
		if pg.hasBadgeImage {
			xobjRes = " /XObject << /Im1 6 0 R >>"
		}
		pageDict := fmt.Sprintf(
			"<< /Type /Page /Parent 2 0 R /MediaBox [ 0 0 %.2f %.2f ] /Resources << /Font << /F1 3 0 R /F2 4 0 R /F3 5 0 R >>%s >> /Contents %d 0 R >>",
			pg.Width, pg.Height, xobjRes, contentID,
		)
		writeObj(pageID, pageDict)

		streamBytes := pg.buf.Bytes()
		offsets[contentID] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n<< /Length %d >>\nstream\n", contentID, len(streamBytes))
		out.Write(streamBytes)
		if len(streamBytes) == 0 || streamBytes[len(streamBytes)-1] != '\n' {
			out.WriteByte('\n')
		}
		out.WriteString("endstream\nendobj\n")
	}

	xrefOffset := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", totalObjs+1)
	for id := 1; id <= totalObjs; id++ {
		fmt.Fprintf(&out, "%010d 00000 n \n", offsets[id])
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		totalObjs+1, xrefOffset)
	return out.Bytes()
}

// Theme defines a color palette for generated PDF documents and reports.
type Theme struct {
	Name   string
	Bg     RGB
	CardBg RGB
	Border RGB
	Title  RGB
	Text   RGB
	Muted  RGB
	C1     RGB
	C2     RGB
	C3     RGB
	C4     RGB
}

// Themes provides selectable palettes for interactive PDF generation.
var Themes = []Theme{
	{
		Name:   "midnight",
		Bg:     Hex("#0d1b2a"),
		CardBg: Hex("#16263b"),
		Border: Hex("#2b4464"),
		Title:  Hex("#8de3b6"),
		Text:   Hex("#e6edf5"),
		Muted:  Hex("#8fa3bc"),
		C1:     Hex("#4ea8de"),
		C2:     Hex("#8de3b6"),
		C3:     Hex("#f28e2b"),
		C4:     Hex("#e15759"),
	},
	{
		Name:   "paper",
		Bg:     Hex("#f8fafc"),
		CardBg: Hex("#ffffff"),
		Border: Hex("#cbd5e1"),
		Title:  Hex("#0f172a"),
		Text:   Hex("#1e293b"),
		Muted:  Hex("#64748b"),
		C1:     Hex("#2563eb"),
		C2:     Hex("#059669"),
		C3:     Hex("#d97706"),
		C4:     Hex("#dc2626"),
	},
	{
		Name:   "emerald",
		Bg:     Hex("#081c15"),
		CardBg: Hex("#123126"),
		Border: Hex("#2d6a4f"),
		Title:  Hex("#95d5b2"),
		Text:   Hex("#d8f3dc"),
		Muted:  Hex("#74c69d"),
		C1:     Hex("#52b788"),
		C2:     Hex("#ffd166"),
		C3:     Hex("#4ea8de"),
		C4:     Hex("#f4a261"),
	},
}

// Series represents one data series in a generated PDF chart.
type Series struct {
	Name   string
	Color  RGB
	Values []float64
}

// DrawBarChart renders a vector grouped bar chart onto pg inside (x, y, w, h).
func DrawBarChart(pg *Page, x, y, w, h float64, th Theme, title string, labels []string, series []Series) {
	pg.RectFillStroke(x, y, w, h, 1.2, th.CardBg, th.Border)
	pg.Text(x+16, y+h-22, FontSansBold, 12, th.Title, title)

	// Legend on top-right
	lx := x + w - 16
	for i := len(series) - 1; i >= 0; i-- {
		s := series[i]
		tw := float64(len(s.Name))*6.5 + 20
		lx -= tw
		pg.RectFill(lx, y+h-23, 10, 10, s.Color)
		pg.Text(lx+14, y+h-22, FontSans, 9.5, th.Text, s.Name)
	}

	plotX := x + 44
	plotY := y + 28
	plotW := w - 60
	plotH := h - 62

	maxVal := 1.0
	for _, s := range series {
		for _, v := range s.Values {
			if v > maxVal {
				maxVal = v
			}
		}
	}
	maxVal = math.Ceil(maxVal*1.15/5.0) * 5.0

	for g := 0; g <= 4; g++ {
		gy := plotY + (plotH*float64(g))/4.0
		if g == 0 {
			pg.Line(plotX, gy, plotX+plotW, gy, 1.2, th.Border)
		} else {
			pg.DashedLine(plotX, gy, plotX+plotW, gy, 0.7, th.Border)
		}
		lbl := fmt.Sprintf("%.0f", (maxVal*float64(g))/4.0)
		pg.Text(x+12, gy-3, FontMono, 8.5, th.Muted, lbl)
	}

	nGroups := len(labels)
	if nGroups == 0 {
		return
	}
	groupW := plotW / float64(nGroups)
	nSeries := max(1, len(series))
	barW := min(26.0, (groupW*0.68)/float64(nSeries))
	clusterW := barW * float64(nSeries)

	for gi, label := range labels {
		gx := plotX + float64(gi)*groupW + (groupW-clusterW)/2.0
		for si, s := range series {
			if gi >= len(s.Values) {
				continue
			}
			v := s.Values[gi]
			bh := max(2.0, (v/maxVal)*plotH)
			bx := gx + float64(si)*barW
			pg.RectFill(bx, plotY, barW-2, bh, s.Color)
		}
		pg.Text(plotX+float64(gi)*groupW+groupW*0.32, plotY-15, FontSansBold, 9.5, th.Text, label)
	}
}

// DrawLineChart renders a vector multi-series line chart onto pg inside (x, y, w, h).
func DrawLineChart(pg *Page, x, y, w, h float64, th Theme, title string, labels []string, series []Series) {
	pg.RectFillStroke(x, y, w, h, 1.2, th.CardBg, th.Border)
	pg.Text(x+16, y+h-22, FontSansBold, 12, th.Title, title)

	lx := x + w - 16
	for i := len(series) - 1; i >= 0; i-- {
		s := series[i]
		tw := float64(len(s.Name))*6.5 + 20
		lx -= tw
		pg.RectFill(lx, y+h-23, 10, 10, s.Color)
		pg.Text(lx+14, y+h-22, FontSans, 9.5, th.Text, s.Name)
	}

	plotX := x + 44
	plotY := y + 28
	plotW := w - 60
	plotH := h - 62

	maxVal := 1.0
	for _, s := range series {
		for _, v := range s.Values {
			if v > maxVal {
				maxVal = v
			}
		}
	}
	maxVal = math.Ceil(maxVal*1.15/5.0) * 5.0

	for g := 0; g <= 4; g++ {
		gy := plotY + (plotH*float64(g))/4.0
		if g == 0 {
			pg.Line(plotX, gy, plotX+plotW, gy, 1.2, th.Border)
		} else {
			pg.DashedLine(plotX, gy, plotX+plotW, gy, 0.7, th.Border)
		}
		lbl := fmt.Sprintf("%.0f", (maxVal*float64(g))/4.0)
		pg.Text(x+12, gy-3, FontMono, 8.5, th.Muted, lbl)
	}

	nPts := len(labels)
	if nPts == 0 {
		return
	}
	stepX := plotW / float64(max(1, nPts-1))
	if nPts == 1 {
		stepX = 0
	}

	for _, s := range series {
		pts := make([][2]float64, 0, len(s.Values))
		for i, v := range s.Values {
			px := plotX + float64(i)*stepX
			py := plotY + (v/maxVal)*plotH
			pts = append(pts, [2]float64{px, py})
		}
		pg.Polyline(pts, 2.6, s.Color)
		for _, pt := range pts {
			pg.CircleFill(pt[0], pt[1], 4.0, s.Color)
		}
	}
	for i, label := range labels {
		px := plotX + float64(i)*stepX - 10
		pg.Text(px, plotY-15, FontSansBold, 9.5, th.Text, label)
	}
}
