package pdfdoc

import "fmt"

// Sample holds a named PDF document in memory for gallery and inspector demos.
type Sample struct {
	Name        string
	Description string
	Data        []byte
}

// ReportDataset describes a telemetry/business dataset for the live PDF report demo.
type ReportDataset struct {
	Key      string
	Title    string
	Subtitle string
	Unit     string
	Labels   []string
	Series   []struct {
		Name   string
		Values []float64
	}
}

// ReportDatasets provides selectable datasets for the live PDF report generator.
var ReportDatasets = []ReportDataset{
	{
		Key:      "revenue",
		Title:    "Quarterly Platform Revenue ($M)",
		Subtitle: "Annual recurring & usage revenue by fiscal quarter",
		Unit:     "$M",
		Labels:   []string{"Q1", "Q2", "Q3", "Q4"},
		Series: []struct {
			Name   string
			Values []float64
		}{
			{Name: "2025", Values: []float64{14.0, 19.5, 16.0, 24.0}},
			{Name: "2026", Values: []float64{18.5, 22.0, 25.5, 31.0}},
		},
	},
	{
		Key:      "latency",
		Title:    "PDF Page Raster Latency (ms)",
		Subtitle: "End-to-end page render & Kitty frame preparation",
		Unit:     "ms",
		Labels:   []string{"72 DPI", "110 DPI", "150 DPI", "220 DPI"},
		Series: []struct {
			Name   string
			Values []float64
		}{
			{Name: "p50", Values: []float64{2.4, 3.8, 5.9, 9.4}},
			{Name: "p95", Values: []float64{4.1, 6.2, 9.8, 15.2}},
			{Name: "p99", Values: []float64{5.8, 8.9, 13.5, 21.0}},
		},
	},
	{
		Key:      "throughput",
		Title:    "Terminal Graphics Throughput (fps)",
		Subtitle: "Sustained viewport pan & page-cache hit rates",
		Unit:     "fps",
		Labels:   []string{"Glyph", "Kitty PNG", "SharedMem", "Cache Hit"},
		Series: []struct {
			Name   string
			Values []float64
		}{
			{Name: "Native", Values: []float64{58, 60, 60, 120}},
			{Name: "Browser", Values: []float64{54, 45, 60, 112}},
		},
	},
}

// GallerySamples returns the curated set of PDF documents for the gallery and
// side-by-side inspector demos.
func GallerySamples(examplePDF []byte) []Sample {
	samples := []Sample{
		{
			Name:        "01-architecture-deck.pdf",
			Description: "3-page 16:9 widescreen architecture deck",
			Data:        BuildArchitectureDeck(),
		},
		{
			Name:        "02-telemetry-spec.pdf",
			Description: "2-page portrait RFC spec with charts & badge image",
			Data:        BuildTelemetrySpec(),
		},
		{
			Name:        "03-cloud-invoice.pdf",
			Description: "2-page financial statement & usage report",
			Data:        BuildCloudInvoice(),
		},
	}
	if len(examplePDF) > 0 {
		samples = append(samples, Sample{
			Name:        "04-example.pdf",
			Description: "Multi-page sample PDF fixture with embedded images",
			Data:        examplePDF,
		})
	} else {
		samples = append(samples, Sample{
			Name:        "04-aspect-calibration.pdf",
			Description: "3-page multi-aspect calibration deck (16:9, Letter, 1:1)",
			Data:        BuildAspectSample(),
		})
	}
	return samples
}

// BuildArchitectureDeck generates a 3-page 16:9 widescreen presentation PDF (720x405 pts).
func BuildArchitectureDeck() []byte {
	b := New()
	b.Title = "Terminal PDF Architecture"
	b.Author = "NimbleTerminal"
	b.Subject = "Architecture Deck"
	th := Themes[0] // midnight

	// Slide 1: Architecture Pipeline
	p1 := b.AddPage(720, 405)
	p1.Background(th.Bg)
	p1.RectFillStroke(24, 336, 672, 48, 1.2, th.CardBg, th.Border)
	p1.DrawBadgeImage(36, 346, 28, 28)
	p1.Text(76, 362, FontSansBold, 16, th.Title, "NTPDF  |  TERMINAL PDF ARCHITECTURE")
	p1.Text(76, 346, FontSans, 10, th.Muted, "Dual-pipeline PDF viewer for Bubble Tea v2 & nimbleterminal.dev/ntpicture")
	p1.Text(624, 355, FontMono, 10, th.C2, "SLIDE 1 / 3")

	stages := []struct {
		x        float64
		step     string
		title    string
		line1    string
		line2    string
		line3    string
		accent   RGB
	}{
		{32, "STAGE 01", "Document Loader", "ledongthuc/pdf text runs", "PDFium WASM / JS bridge", "Resource & DoS guardrails", th.C1},
		{268, "STAGE 02", "Viewport & Cache", "LRU page bitmap cache", "Dynamic DPI auto-scaling", "Normalized zoom & pan", th.C2},
		{504, "STAGE 03", "ntpicture.Picture", "Kitty APC & POSIX SHM", "Browser shared-memory RGBA", "Half-block glyph fallback", th.C3},
	}
	for i, st := range stages {
		p1.RectFillStroke(st.x, 146, 184, 162, 1.5, th.CardBg, st.accent)
		p1.RectFill(st.x, 280, 184, 28, st.accent)
		p1.Text(st.x+14, 290, FontMono, 10, th.Bg, st.step)
		p1.Text(st.x+14, 252, FontSansBold, 13, th.Text, st.title)
		p1.Text(st.x+14, 222, FontSans, 10, th.Muted, st.line1)
		p1.Text(st.x+14, 200, FontSans, 10, th.Muted, st.line2)
		p1.Text(st.x+14, 178, FontSans, 10, th.Muted, st.line3)
		if i < len(stages)-1 {
			ax := st.x + 190
			p1.Line(ax, 226, ax+40, 226, 2.5, th.Title)
			p1.CircleFill(ax+40, 226, 4.0, th.Title)
		}
	}

	p1.RectFillStroke(32, 32, 656, 88, 1.2, th.CardBg, th.Border)
	p1.Text(48, 92, FontSansBold, 11, th.Title, "KEY RUNTIME CAPABILITIES")
	p1.Text(48, 68, FontMono, 9.5, th.Text, "* Zero-CGO native PDFium via wazero    * Browser WASM shared-memory transport")
	p1.Text(48, 48, FontMono, 9.5, th.Text, "* Instant TextMode extraction (m)      * FitContain / FitFill / FitCover (f)")

	// Slide 2: Dual Rendering Modes
	p2 := b.AddPage(720, 405)
	p2.Background(th.Bg)
	p2.RectFillStroke(24, 336, 672, 48, 1.2, th.CardBg, th.Border)
	p2.Text(40, 362, FontSansBold, 16, th.Title, "IMAGE MODE vs TEXT MODE PIPELINES")
	p2.Text(40, 346, FontSans, 10, th.Muted, "Press m to switch between full-page rasterization and spatial text projection")
	p2.Text(624, 355, FontMono, 10, th.C2, "SLIDE 2 / 3")

	// Left card: ImageMode
	p2.RectFillStroke(32, 40, 316, 274, 1.5, th.CardBg, th.C1)
	p2.Text(50, 286, FontSansBold, 14, th.C1, "ImageMode  (PDFium + ntpicture)")
	p2.Line(50, 274, 330, 274, 1.0, th.Border)
	p2.Text(50, 248, FontSans, 10.5, th.Text, "1. Rasterizes page at RenderDPI / DynamicDPI")
	p2.Text(50, 224, FontSans, 10.5, th.Text, "2. Caches uncropped RGBA bitmap in LRU cache")
	p2.Text(50, 200, FontSans, 10.5, th.Text, "3. Crops normalized (zoom, panX, panY) window")
	p2.Text(50, 176, FontSans, 10.5, th.Text, "4. Submits immutable frame to ntpicture.Picture")
	p2.RectFillStroke(50, 62, 280, 92, 1.0, th.Bg, th.Border)
	p2.Text(64, 130, FontMono, 9.5, th.C2, "Protocols:")
	p2.Text(64, 108, FontMono, 9.0, th.Text, "- RenderKitty: APC PNG / SharedMem RGBA")
	p2.Text(64, 88, FontMono, 9.0, th.Text, "- RenderGlyph: Unicode half-block cells")

	// Right card: TextMode
	p2.RectFillStroke(372, 40, 316, 274, 1.5, th.CardBg, th.C2)
	p2.Text(390, 286, FontSansBold, 14, th.C2, "TextMode  (Spatial Rune Grid)")
	p2.Line(390, 274, 670, 274, 1.0, th.Border)
	p2.Text(390, 248, FontSans, 10.5, th.Text, "1. Extracts positioned pdf.Text glyph runs")
	p2.Text(390, 224, FontSans, 10.5, th.Text, "2. Projects (X, Y) onto (cols x rows) grid")
	p2.Text(390, 200, FontSans, 10.5, th.Text, "3. Packs intra-word glyphs; keeps column gaps")
	p2.Text(390, 176, FontSans, 10.5, th.Text, "4. Counts /Subtype /Image XObjects as badges")
	p2.RectFillStroke(390, 62, 280, 92, 1.0, th.Bg, th.Border)
	p2.Text(404, 130, FontMono, 9.5, th.C3, "Security & Fallbacks:")
	p2.Text(404, 108, FontMono, 9.0, th.Text, "- Strips non-printable & bidi control runes")
	p2.Text(404, 88, FontMono, 9.0, th.Text, "- Works even when RendererFactory is nil")

	// Slide 3: Benchmarks & Guardrails
	p3 := b.AddPage(720, 405)
	p3.Background(th.Bg)
	p3.RectFillStroke(24, 336, 672, 48, 1.2, th.CardBg, th.Border)
	p3.Text(40, 362, FontSansBold, 16, th.Title, "THROUGHPUT & RESOURCE GUARDRAILS")
	p3.Text(40, 346, FontSans, 10, th.Muted, "Configurable Limits protect against decompression bombs and runaway allocations")
	p3.Text(624, 355, FontMono, 10, th.C2, "SLIDE 3 / 3")

	DrawBarChart(p3, 32, 40, 360, 274, th, "Frame Throughput (fps)",
		[]string{"Glyph", "Kitty PNG", "SharedMem", "Cache Hit"},
		[]Series{
			{Name: "Native", Color: th.C1, Values: []float64{58, 60, 60, 120}},
			{Name: "Browser", Color: th.C2, Values: []float64{54, 45, 60, 112}},
		},
	)

	p3.RectFillStroke(412, 40, 276, 274, 1.2, th.CardBg, th.Border)
	p3.Text(428, 286, FontSansBold, 13, th.Title, "DEFAULT SAFETY LIMITS")
	p3.Line(428, 272, 672, 272, 1.0, th.Border)
	limits := [][2]string{
		{"MaxFileBytes", "64 MiB"},
		{"MaxPages", "2000 pages"},
		{"MaxRenderDPI", "300 DPI"},
		{"MaxPagePixels", "16 MP / page"},
		{"MaxImagesPerPage", "1024 images"},
		{"PageCacheSize", "3 pages (LRU)"},
	}
	for i, kv := range limits {
		ry := 244.0 - float64(i)*32.0
		p3.Text(428, ry, FontMono, 9.5, th.C1, kv[0])
		p3.Text(572, ry, FontMono, 9.5, th.Text, kv[1])
	}

	return b.Bytes()
}

// BuildTelemetrySpec generates a 2-page US Letter engineering specification PDF (612x792 pts).
func BuildTelemetrySpec() []byte {
	b := New()
	b.Title = "RFC-2026: Terminal Document Streaming"
	b.Author = "NimbleTerminal"
	b.Subject = "Standards Track Specification"
	th := Themes[1] // paper

	p1 := b.AddPage(612, 792)
	p1.Background(th.Bg)
	p1.RectFillStroke(36, 700, 540, 60, 1.2, th.CardBg, th.Border)
	p1.DrawBadgeImage(48, 714, 32, 32)
	p1.Text(92, 734, FontSansBold, 15, th.Title, "RFC-2026: TERMINAL DOCUMENT STREAMING")
	p1.Text(92, 714, FontMono, 9.5, th.Muted, "Category: Standards Track   |   Status: Verified   |   Page 1 of 2")

	// Abstract box
	p1.RectFillStroke(36, 606, 540, 78, 1.0, th.CardBg, th.Border)
	p1.Text(50, 662, FontSansBold, 11, th.C1, "1. ABSTRACT")
	p1.Text(50, 642, FontSans, 10, th.Text, "This specification defines a zero-flicker pipeline for rendering multi-page PDF")
	p1.Text(50, 626, FontSans, 10, th.Text, "documents inside terminal emulators using Kitty graphics and spatial rune grids.")

	// Two columns
	p1.RectFillStroke(36, 350, 260, 240, 1.0, th.CardBg, th.Border)
	p1.Text(50, 566, FontSansBold, 11, th.C1, "2. RASTER PIPELINE")
	p1.Text(50, 542, FontSans, 9.5, th.Text, "Pages are rasterized off the UI")
	p1.Text(50, 526, FontSans, 9.5, th.Text, "goroutine via a per-document")
	p1.Text(50, 510, FontSans, 9.5, th.Text, "Renderer. Generation counters")
	p1.Text(50, 494, FontSans, 9.5, th.Text, "(loadGen, renderGen) drop stale")
	p1.Text(50, 478, FontSans, 9.5, th.Text, "completions when flipping pages")
	p1.Text(50, 462, FontSans, 9.5, th.Text, "rapidly.")
	p1.Text(50, 430, FontMono, 9.0, th.C2, "zoom 0 -> SetImageImmutable")
	p1.Text(50, 412, FontMono, 9.0, th.C2, "zoom N -> SubImage crop 1/2^N")

	p1.RectFillStroke(316, 350, 260, 240, 1.0, th.CardBg, th.Border)
	p1.Text(330, 566, FontSansBold, 11, th.C2, "3. DYNAMIC DPI SCALING")
	p1.Text(330, 542, FontSans, 9.5, th.Text, "When DynamicDPI is enabled, the")
	p1.Text(330, 526, FontSans, 9.5, th.Text, "widget inspects CellPixelSize()")
	p1.Text(330, 510, FontSans, 9.5, th.Text, "and the page MediaBox in points")
	p1.Text(330, 494, FontSans, 9.5, th.Text, "to compute the exact DPI needed")
	p1.Text(330, 478, FontSans, 9.5, th.Text, "to fill the terminal cell rect")
	p1.Text(330, 462, FontSans, 9.5, th.Text, "without oversampling.")
	p1.Text(330, 430, FontMono, 9.0, th.C1, "dpiW = (cols * cw * 72) / wPts")
	p1.Text(330, 412, FontMono, 9.0, th.C1, "dpiH = (rows * ch * 72) / hPts")

	DrawLineChart(p1, 36, 48, 540, 284, th, "4. RENDER LATENCY BY DPI (ms)",
		[]string{"72 DPI", "110 DPI", "150 DPI", "220 DPI"},
		[]Series{
			{Name: "p50", Color: th.C2, Values: []float64{2.4, 3.8, 5.9, 9.4}},
			{Name: "p95", Color: th.C3, Values: []float64{4.1, 6.2, 9.8, 15.2}},
			{Name: "p99", Color: th.C4, Values: []float64{5.8, 8.9, 13.5, 21.0}},
		},
	)

	// Page 2: Protocol State & Verification Table
	p2 := b.AddPage(612, 792)
	p2.Background(th.Bg)
	p2.RectFillStroke(36, 700, 540, 60, 1.2, th.CardBg, th.Border)
	p2.Text(52, 734, FontSansBold, 15, th.Title, "RFC-2026: PROTOCOL MATRIX & BENCHMARKS")
	p2.Text(52, 714, FontMono, 9.5, th.Muted, "Section 5: Transport Modes & Verification   |   Page 2 of 2")

	p2.RectFillStroke(36, 420, 540, 260, 1.0, th.CardBg, th.Border)
	p2.Text(52, 654, FontSansBold, 12, th.Title, "5. TRANSPORT & FIT MODE MATRIX")
	p2.Line(52, 640, 560, 640, 1.0, th.Border)
	p2.Text(52, 620, FontMono, 9.5, th.C1, "MODE             TRANSPORT             BEHAVIOR")
	rows := [][3]string{
		{"RenderKitty", "Direct APC (PNG)", "Chunked base64 PNG over Kitty APC"},
		{"RenderKitty", "POSIX / JS SharedMem", "Zero-copy RGBA shared memory buffer"},
		{"RenderGlyph", "Unicode Half-Blocks", "Universal 24-bit ANSI fallback"},
		{"FitContain", "Letterboxed Aspect", "Preserves page aspect ratio"},
		{"FitFill", "Stretched Cell Rect", "Maps full page to cell grid"},
		{"FitCover", "Anchored Crop", "Fills cell grid from FitAnchor"},
	}
	for i, r := range rows {
		ry := 592.0 - float64(i)*26.0
		p2.Text(52, ry, FontMono, 9.0, th.Text, r[0])
		p2.Text(168, ry, FontMono, 9.0, th.C2, r[1])
		p2.Text(316, ry, FontSans, 9.5, th.Text, r[2])
	}

	DrawBarChart(p2, 36, 56, 540, 340, th, "6. CACHE & TRANSPORT THROUGHPUT (FPS)",
		[]string{"Glyph", "Kitty PNG", "SharedMem", "Cache Hit"},
		[]Series{
			{Name: "Native", Color: th.C1, Values: []float64{58, 60, 60, 120}},
			{Name: "Browser", Color: th.C2, Values: []float64{54, 45, 60, 112}},
		},
	)

	return b.Bytes()
}

// BuildCloudInvoice generates a 2-page financial statement and usage report PDF (612x792 pts).
func BuildCloudInvoice() []byte {
	b := New()
	b.Title = "Nimble Markets Cloud Invoice #INV-2026-04"
	b.Author = "Nimble Markets Billing"
	b.Subject = "Quarterly Statement"
	th := Themes[1] // paper

	p1 := b.AddPage(612, 792)
	p1.Background(th.Bg)
	p1.RectFillStroke(36, 688, 540, 72, 1.2, th.CardBg, th.Border)
	p1.DrawBadgeImage(50, 706, 36, 36)
	p1.Text(100, 734, FontSansBold, 16, th.Title, "NIMBLE MARKETS CLOUD  |  INVOICE #INV-2026-04")
	p1.Text(100, 712, FontMono, 9.5, th.Muted, "Billing Period: 2026-Q4   |   Account: NM-88412   |   Status: PAID")

	// KPI Cards
	kpis := []struct {
		x     float64
		label string
		val   string
		sub   string
		color RGB
	}{
		{36, "SUBTOTAL", "$24,800.00", "Compute & Rendering", th.C1},
		{222, "CREDITS APPLIED", "-$1,800.00", "Committed Use Tier", th.C2},
		{408, "TOTAL DUE", "$23,000.00", "Paid via ACH", th.C3},
	}
	for _, k := range kpis {
		p1.RectFillStroke(k.x, 594, 168, 76, 1.2, th.CardBg, k.color)
		p1.Text(k.x+14, 650, FontMono, 9.0, th.Muted, k.label)
		p1.Text(k.x+14, 624, FontSansBold, 16, k.color, k.val)
		p1.Text(k.x+14, 604, FontSans, 9.0, th.Text, k.sub)
	}

	// Line items table
	p1.RectFillStroke(36, 336, 540, 240, 1.0, th.CardBg, th.Border)
	p1.Text(52, 552, FontSansBold, 12, th.Title, "ITEMIZED SERVICE CHARGES")
	p1.Line(52, 538, 560, 538, 1.0, th.Border)
	p1.Text(52, 520, FontMono, 9.5, th.C1, "SKU          SERVICE DESCRIPTION                QTY          AMOUNT")
	items := [][4]string{
		{"NM-PDF-RT", "Dedicated PDFium Worker Pool", "12 nodes", "$9,600.00"},
		{"NM-KIT-SHM", "High-Throughput Edge Bandwidth", "48 TB", "$6,200.00"},
		{"NM-SVG-STG", "Vector Telemetry Stage Cluster", "8 nodes", "$5,400.00"},
		{"NM-ARC-S3", "Encrypted Document Vault", "120 TB", "$3,600.00"},
		{"NM-CRD-26", "Annual Platform Commitment Credit", "1 tier", "-$1,800.00"},
	}
	for i, it := range items {
		ry := 490.0 - float64(i)*28.0
		p1.Text(52, ry, FontMono, 9.5, th.Muted, it[0])
		p1.Text(140, ry, FontSans, 10.0, th.Text, it[1])
		p1.Text(380, ry, FontMono, 9.5, th.Text, it[2])
		p1.Text(476, ry, FontMono, 9.5, th.Title, it[3])
	}

	DrawBarChart(p1, 36, 48, 540, 270, th, "QUARTERLY SPEND HISTORY ($K)",
		[]string{"Q1", "Q2", "Q3", "Q4"},
		[]Series{
			{Name: "Compute", Color: th.C1, Values: []float64{12.5, 14.0, 15.2, 15.0}},
			{Name: "Storage & Edge", Color: th.C2, Values: []float64{6.2, 7.1, 7.8, 8.0}},
		},
	)

	// Page 2: Regional Breakdown
	p2 := b.AddPage(612, 792)
	p2.Background(th.Bg)
	p2.RectFillStroke(36, 688, 540, 72, 1.2, th.CardBg, th.Border)
	p2.Text(52, 734, FontSansBold, 16, th.Title, "USAGE TELEMETRY & REGIONAL BREAKDOWN")
	p2.Text(52, 712, FontMono, 9.5, th.Muted, "Invoice #INV-2026-04   |   Appendix: Cluster Utilization   |   Page 2 of 2")

	DrawLineChart(p2, 36, 368, 540, 298, th, "DAILY DOCUMENT RENDERS (THOUSANDS)",
		[]string{"W1", "W2", "W3", "W4"},
		[]Series{
			{Name: "us-east", Color: th.C1, Values: []float64{42, 48, 51, 58}},
			{Name: "eu-central", Color: th.C2, Values: []float64{28, 31, 35, 39}},
			{Name: "ap-northeast", Color: th.C3, Values: []float64{19, 22, 24, 29}},
		},
	)

	p2.RectFillStroke(36, 64, 540, 280, 1.0, th.CardBg, th.Border)
	p2.Text(52, 316, FontSansBold, 12, th.Title, "OPERATIONAL NOTES & SLA SUMMARY")
	p2.Line(52, 302, 560, 302, 1.0, th.Border)
	p2.Text(52, 274, FontSans, 10, th.Text, "* 99.994% render availability across all three regions during Q4.")
	p2.Text(52, 250, FontSans, 10, th.Text, "* Page cache hit ratio averaged 84.2% for interactive terminal sessions.")
	p2.Text(52, 226, FontSans, 10, th.Text, "* Zero-copy shared-memory Kitty frames reduced browser heap churn by 71%.")
	p2.Text(52, 180, FontMono, 9.5, th.C1, "SHA256: 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08")

	return b.Bytes()
}

// BuildReport dynamically constructs a 2-page executive PDF report for the
// interactive report builder demo.
func BuildReport(lineMode bool, datasetIdx, themeIdx, variant int) (string, []byte) {
	d := ReportDatasets[((datasetIdx%len(ReportDatasets))+len(ReportDatasets))%len(ReportDatasets)]
	th := Themes[((themeIdx%len(Themes))+len(Themes))%len(Themes)]
	b := New()
	b.Title = d.Title
	b.Author = "NimbleTerminal Report Builder"
	b.Subject = d.Subtitle

	palette := []RGB{th.C1, th.C2, th.C3, th.C4}
	series := make([]Series, len(d.Series))
	var totalVal, peakVal float64
	for si, s := range d.Series {
		vals := make([]float64, len(s.Values))
		for i, v := range s.Values {
			delta := float64(((variant*(i+2)+si*3)%5)-2) * 0.12 * v
			if variant == 0 {
				delta = 0
			}
			val := max(0.5, v+delta)
			vals[i] = val
			totalVal += val
			if val > peakVal {
				peakVal = val
			}
		}
		series[si] = Series{
			Name:   s.Name,
			Color:  palette[si%len(palette)],
			Values: vals,
		}
	}

	kind := "bar"
	if lineMode {
		kind = "line"
	}

	// Page 1: Widescreen Executive Report (720x450 pts)
	p1 := b.AddPage(720, 450)
	p1.Background(th.Bg)
	p1.RectFillStroke(24, 378, 672, 52, 1.2, th.CardBg, th.Border)
	p1.DrawBadgeImage(36, 390, 28, 28)
	p1.Text(76, 408, FontSansBold, 15, th.Title, "LIVE PDF REPORT BUILDER  |  "+d.Title)
	p1.Text(76, 390, FontSans, 10, th.Muted, fmt.Sprintf("%s   (theme=%s, chart=%s, variant=%d)", d.Subtitle, th.Name, kind, variant))
	p1.Text(620, 400, FontMono, 10, th.C2, "PAGE 1 / 2")

	// KPI Strip
	cards := []struct {
		x     float64
		title string
		value string
		color RGB
	}{
		{24, "DATASET", d.Key, th.C1},
		{256, "PEAK VALUE", fmt.Sprintf("%.1f %s", peakVal, d.Unit), th.C2},
		{488, "AGGREGATE", fmt.Sprintf("%.1f %s", totalVal, d.Unit), th.C3},
	}
	for _, c := range cards {
		p1.RectFillStroke(c.x, 304, 208, 58, 1.2, th.CardBg, c.color)
		p1.Text(c.x+14, 344, FontMono, 9.0, th.Muted, c.title)
		p1.Text(c.x+14, 318, FontSansBold, 15, c.color, c.value)
	}

	if lineMode {
		DrawLineChart(p1, 24, 24, 672, 264, th, d.Title, d.Labels, series)
	} else {
		DrawBarChart(p1, 24, 24, 672, 264, th, d.Title, d.Labels, series)
	}

	// Page 2: Detailed Data Table & Digest
	p2 := b.AddPage(720, 450)
	p2.Background(th.Bg)
	p2.RectFillStroke(24, 378, 672, 52, 1.2, th.CardBg, th.Border)
	p2.Text(40, 408, FontSansBold, 15, th.Title, "TABULAR SERIES BREAKDOWN  |  "+d.Title)
	p2.Text(40, 390, FontSans, 10, th.Muted, "Press m to inspect extracted table text in TextMode")
	p2.Text(620, 400, FontMono, 10, th.C2, "PAGE 2 / 2")

	p2.RectFillStroke(24, 136, 672, 224, 1.2, th.CardBg, th.Border)
	p2.Text(44, 332, FontSansBold, 12, th.Title, "SERIES VALUES BY BUCKET ("+d.Unit+")")
	p2.Line(44, 318, 676, 318, 1.0, th.Border)

	headerLine := fmt.Sprintf("%-14s", "SERIES")
	for _, lbl := range d.Labels {
		headerLine += fmt.Sprintf("  %10s", lbl)
	}
	p2.Text(44, 296, FontMono, 10, th.C1, headerLine)

	for si, s := range series {
		row := fmt.Sprintf("%-14s", s.Name)
		for _, v := range s.Values {
			row += fmt.Sprintf("  %10.1f", v)
		}
		ry := 264.0 - float64(si)*32.0
		p2.Text(44, ry, FontMono, 10, th.Text, row)
	}

	p2.RectFillStroke(24, 24, 672, 96, 1.0, th.CardBg, th.Border)
	p2.Text(44, 94, FontSansBold, 11, th.C2, "REPORT GENERATION NOTES")
	p2.Text(44, 70, FontSans, 10, th.Text, "* Generated in-memory as a standards-compliant PDF 1.4 byte stream.")
	p2.Text(44, 48, FontSans, 10, th.Text, "* Loaded via ntpdf.Model.SetPDFData with zero temporary files.")

	name := fmt.Sprintf("report-%s-%s.pdf", d.Key, kind)
	return name, b.Bytes()
}

// BuildAspectSample generates a 3-page PDF where each page has a distinct
// aspect ratio (Page 1: 16:9 Widescreen 720x405, Page 2: US Letter Portrait
// 612x792, Page 3: 1:1 Square 520x520) for the pagedims aspect-matching demo.
func BuildAspectSample() []byte {
	b := New()
	b.Title = "Multi-Aspect Viewport Calibration"
	b.Author = "NimbleTerminal"
	b.Subject = "Viewport Calibration"
	th := Themes[0] // midnight

	specs := []struct {
		w, h  float64
		label string
		ratio string
		note  string
		color RGB
	}{
		{720, 405, "WIDESCREEN SLIDE", "720 x 405 pts  (16:9 Landscape)", "Wide aspect: cellrect expands horizontally", th.C1},
		{612, 792, "PORTRAIT DOCUMENT", "612 x 792 pts  (8.5 x 11 Letter)", "Tall aspect: cellrect narrows to match portrait page", th.C2},
		{520, 520, "SQUARE TELEMETRY CARD", "520 x 520 pts  (1:1 Square)", "Square pixel aspect -> 2:1 cell width-to-height ratio", th.C3},
	}

	for i, sp := range specs {
		pg := b.AddPage(sp.w, sp.h)
		pg.Background(th.Bg)
		pg.RectFillStroke(16, 16, sp.w-32, sp.h-32, 3.0, th.CardBg, sp.color)
		pg.RectStroke(28, 28, sp.w-56, sp.h-56, 1.0, th.Border)

		// Corner calibration markers so edge-to-edge fit is visually obvious
		m := 38.0
		pg.RectFill(22, sp.h-22-m, m, 8, sp.color)
		pg.RectFill(22, sp.h-22-m, 8, m, sp.color)
		pg.RectFill(sp.w-22-m, sp.h-22-m, m, 8, sp.color)
		pg.RectFill(sp.w-22-8, sp.h-22-m, 8, m, sp.color)
		pg.RectFill(22, 22, m, 8, sp.color)
		pg.RectFill(22, 22, 8, m, sp.color)
		pg.RectFill(sp.w-22-m, 22, m, 8, sp.color)
		pg.RectFill(sp.w-22-8, 22, 8, m, sp.color)

		cy := sp.h / 2.0
		pg.DrawBadgeImage(48, cy+44, 32, 32)
		pg.Text(92, cy+58, FontSansBold, 18, sp.color, fmt.Sprintf("PAGE %d / %d  —  %s", i+1, len(specs), sp.label))
		pg.Text(92, cy+34, FontMono, 12, th.Text, sp.ratio)
		pg.Line(48, cy+16, sp.w-48, cy+16, 1.2, th.Border)
		pg.Text(48, cy-12, FontSans, 11, th.Muted, sp.note)
		pg.Text(48, cy-36, FontMono, 10, th.Title, "Press n/p to switch page aspect ratios, or a to toggle aspect lock.")
	}

	return b.Bytes()
}
