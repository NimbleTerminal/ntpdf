package ntpdf

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"strings"
	"testing"

	"github.com/ledongthuc/pdf"
	"nimbleterminal.dev/ntpdf/pdfdoc"
)

func TestSanitizeForTerminalDropsControlsAndBidi(t *testing.T) {
	// Mix of dangerous runes that have spoofed terminals in the wild.
	cases := map[string]string{
		"plain":             "plain",
		"with\x1b[31m ANSI": "with[31m ANSI", // ESC stripped, brackets kept
		"DEL\x7Fchar":       "DELchar",       // 0x7F dropped
		"C1\u009bsequence":  "C1sequence",    // CSI in C1 dropped (properly UTF-8 encoded)
		"bidi\u202ewolves":  "bidiwolves",    // RLO dropped (Trojan Source)
		"zero\u200bwidth":   "zerowidth",     // ZWSP dropped
		"line\nbreak":       "line break",    // newline → space
		"tab\there":         "tab here",      // tab → space
		"\u202d\u202eboth":  "both",          // LRO + RLO dropped
		"emoji✓ok":          "emoji✓ok",      // emojis are printable, kept
		"":                  "",              // empty
	}
	for in, want := range cases {
		if got := SanitizeForTerminal(in); got != want {
			t.Errorf("SanitizeForTerminal(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLayoutRunsDropsControlsAndBidi(t *testing.T) {
	// A run containing a bidi RLO override and an ESC must not place
	// those runes in the grid — they'd reach the terminal raw and could
	// reorder visual output.
	mb := mediaBox{0, 0, 612, 792}
	hostile := "abc\u202e\x1b[31m" // RLO + ESC + CSI
	runs := []pdf.Text{{X: 50, Y: 700, W: 60, FontSize: 12, S: hostile}}
	grid := layoutRuns(runs, mb, 80, 24)

	for ri, row := range grid {
		for ci, r := range row {
			if r == 0x1B || r == 0x202E {
				t.Errorf("grid[%d][%d] = %U; dangerous rune not filtered", ri, ci, r)
			}
		}
	}
	// Spot-check: visible letters survived.
	if !strings.Contains(joinGrid(grid), "abc") {
		t.Error("printable prefix 'abc' missing from sanitized grid")
	}
}

func TestLimitsApplyDefaults(t *testing.T) {
	var l Limits
	l.applyDefaults()
	if l.MaxFileBytes <= 0 {
		t.Errorf("MaxFileBytes default = %d, want > 0", l.MaxFileBytes)
	}
	if l.MaxPages <= 0 {
		t.Errorf("MaxPages default = %d, want > 0", l.MaxPages)
	}
	if l.MaxImagesPerPage <= 0 {
		t.Errorf("MaxImagesPerPage default = %d, want > 0", l.MaxImagesPerPage)
	}
	if l.MaxRenderDPI <= 0 {
		t.Errorf("MaxRenderDPI default = %d, want > 0", l.MaxRenderDPI)
	}
	if l.MaxRenderPixels <= 0 {
		t.Errorf("MaxRenderPixels default = %d, want > 0", l.MaxRenderPixels)
	}

	// Negative values are preserved verbatim — the opt-out sentinel.
	l = Limits{MaxPages: -1, MaxRenderDPI: -1}
	l.applyDefaults()
	if l.MaxPages != -1 || l.MaxRenderDPI != -1 {
		t.Errorf("applyDefaults overwrote negative sentinels: %+v", l)
	}
}

// dpiCapturingRenderer records the dpi argument it was asked to render
// at — used to verify withLimits clamping.
type dpiCapturingRenderer struct {
	lastDPI int
}

func (r *dpiCapturingRenderer) RenderPage(_ int, dpi int) (image.Image, error) {
	r.lastDPI = dpi
	return image.NewRGBA(image.Rect(0, 0, 1, 1)), nil
}
func (r *dpiCapturingRenderer) Close() error { return nil }

func TestWithLimitsClampsDPI(t *testing.T) {
	// Inner factory always succeeds — wrap it with a 150-DPI cap and
	// verify a render request at 600 reaches the inner with 150.
	inner := func(string, []byte) (Renderer, error) { return &dpiCapturingRenderer{}, nil }
	wrapped := withLimits(inner, Limits{MaxRenderDPI: 150})

	r, err := wrapped("ignored", nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	defer r.Close()
	if _, err := r.RenderPage(1, 600); err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	// The inner is the dpiCapturingRenderer wrapped by limitedRenderer —
	// unwrap once to inspect.
	inner2, ok := r.(*limitedRenderer)
	if !ok {
		t.Fatalf("expected *limitedRenderer, got %T", r)
	}
	cap, ok := inner2.inner.(*dpiCapturingRenderer)
	if !ok {
		t.Fatalf("inner type = %T", inner2.inner)
	}
	if cap.lastDPI != 150 {
		t.Errorf("inner DPI = %d, want 150 (clamped from 600)", cap.lastDPI)
	}

	// Also verify ContextRenderer + RenderPageContext clamping and pre-cancel behavior.
	cr, ok := r.(ContextRenderer)
	if !ok {
		t.Fatalf("limitedRenderer does not implement ContextRenderer")
	}
	if _, err := cr.RenderPageContext(context.Background(), 1, 900); err != nil {
		t.Fatalf("RenderPageContext: %v", err)
	}
	if cap.lastDPI != 150 {
		t.Errorf("inner DPI via RenderPageContext = %d, want 150", cap.lastDPI)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RenderPageContext(ctx, r, 1, 100); !errors.Is(err, context.Canceled) {
		t.Errorf("RenderPageContext with cancelled ctx = %v, want context.Canceled", err)
	}
}

func TestWithLimitsDoesNotStatPath(t *testing.T) {
	// Regression: withLimits used to call os.Stat(path), breaking
	// custom factories that pass virtual identifiers (URLs, IDs, blob
	// handles). The wrapper now only clamps DPI; MaxFileBytes is
	// enforced by the default-filesystem renderer.
	inner := func(name string, data []byte) (Renderer, error) {
		if name != "id://abc-123" {
			t.Errorf("inner factory received unexpected name %q", name)
		}
		if len(data) != 4 || string(data) != "blob" {
			t.Errorf("inner factory received unexpected data %q", data)
		}
		return &dpiCapturingRenderer{}, nil
	}
	wrapped := withLimits(inner, Limits{MaxFileBytes: 1, MaxRenderDPI: 150})
	r, err := wrapped("id://abc-123", []byte("blob"))
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	_ = r.Close()
}

func TestExtractPageRecoversFromPanic(t *testing.T) {
	path := repoTestdata(t, "Example.pdf")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := pdfdoc.Open(data, pdfdoc.Limits{MaxImageDraws: 1024})
	if err != nil {
		t.Fatal(err)
	}
	// Nil-doc and out-of-range page inputs must return the default media box
	// and zero runs without panicking.
	for _, out := range []pdfPage{extractPage(nil, 1), extractPage(doc, 0)} {
		if out.media != defaultMediaBox() {
			t.Errorf("invalid page: media = %+v, want default", out.media)
		}
		if len(out.runs) != 0 {
			t.Errorf("invalid page: runs len = %d, want 0", len(out.runs))
		}
	}
}

func TestMaxPagesRejected(t *testing.T) {
	path := repoTestdata(t, "Example.pdf")
	// Example.pdf has 3 pages; MaxPages=2 should reject.
	m := NewWithConfig(Config{Cols: 80, Rows: 24, Limits: Limits{MaxPages: 2}})
	cmd := m.SetPDF(path)
	if cmd == nil {
		t.Fatal("nil cmd")
	}
	switch msg := cmd().(type) {
	case pdfErrMsg:
		if !strings.Contains(msg.err.Error(), "MaxPages") {
			t.Errorf("error %q lacks MaxPages context", msg.err)
		}
	case pdfLoadedMsg:
		t.Errorf("expected pdfErrMsg, got loaded with %d pages", len(msg.pages))
	default:
		t.Errorf("unexpected msg %T", msg)
	}
}

func TestCountPageImagesHonorsCap(t *testing.T) {
	// Surrogate: PageImages on Example.pdf page 1 (1 image) with a
	// tiny cap returns at most cap. We only verify it doesn't exceed.
	path := repoTestdata(t, "Example.pdf")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := pdfdoc.Open(data, pdfdoc.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	got := doc.PageImages(1)
	if got != 1 {
		t.Fatalf("baseline: expected 1 image on page 1, got %d", got)
	}
	capped, err := pdfdoc.Open(data, pdfdoc.Limits{MaxImageDraws: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got := capped.PageImages(1); got > 1 {
		t.Errorf("cap=1 but counted %d", got)
	}
}

func TestTextModeRejectsHostilePDFs(t *testing.T) {
	deflate := func(raw []byte) []byte {
		var b bytes.Buffer
		w := zlib.NewWriter(&b)
		if _, err := w.Write(raw); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	buildPDF := func(trailer string, objects ...string) []byte {
		var b bytes.Buffer
		b.WriteString("%PDF-1.4\n")
		offsets := make([]int, len(objects))
		for i, body := range objects {
			offsets[i] = b.Len()
			fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, body)
		}
		xref := b.Len()
		fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
		for _, off := range offsets {
			fmt.Fprintf(&b, "%010d 00000 n \n", off)
		}
		fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R %s >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, trailer, xref)
		return b.Bytes()
	}

	// 1. Oversized Flate inflation: 8 KiB decoded stream rejected when MaxStreamBytes=1024.
	inflated := deflate(bytes.Repeat([]byte("A"), 8192))
	oversizedStream := buildPDF("",
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 72 72] /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d /Filter /FlateDecode >>\nstream\n%s\nendstream", len(inflated), inflated),
	)

	// 2. Bad predictor parameters on a Flate stream in a valid PDF shell.
	comp := deflate([]byte("q Q\n"))
	badPredictor := buildPDF("",
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 72 72] /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d /Filter /FlateDecode /DecodeParms << /Predictor 12 /Columns 2147483647 >> >>\nstream\n%s\nendstream", len(comp), comp),
	)

	// 3. Hidden xref inside a string referenced by /Prev.
	hiddenObjs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 72 72] >>",
		"(xref\n0 1\n0000000000 65535 f \ntrailer\n<< /Size 1 >>\n%)",
	}
	baseHidden := buildPDF("", hiddenObjs...)
	hiddenOffset := bytes.Index(baseHidden, []byte("xref\n"))
	hiddenXref := buildPDF(fmt.Sprintf("/Prev %d", hiddenOffset), hiddenObjs...)

	// 4. Hidden object inside a string literal referenced by the xref table.
	spoofBase := buildPDF("",
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 72 72] >>",
		"(ordinary)",
		"(4 0 obj\n<< /Length 3 >>\nstream\nabc\nendstream\nendobj)",
	)
	real4 := bytes.Index(spoofBase, []byte("4 0 obj"))
	fake4 := bytes.LastIndex(spoofBase, []byte("4 0 obj"))
	hiddenObj := bytes.Replace(spoofBase, fmt.Appendf(nil, "%010d 00000 n", real4), fmt.Appendf(nil, "%010d 00000 n", fake4), 1)

	for _, tc := range []struct {
		name   string
		data   []byte
		limits Limits
	}{
		{"oversized inflate", oversizedStream, Limits{MaxStreamBytes: 1024}},
		{"bad predictor", badPredictor, Limits{}},
		{"hidden xref", hiddenXref, Limits{}},
		{"hidden object", hiddenObj, Limits{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Verify the raw ledongthuc/pdf reader (which ntpdf used to call
			// directly without preflight) accepts the document.
			if _, err := pdf.NewReader(bytes.NewReader(tc.data), int64(len(tc.data))); err != nil {
				t.Fatalf("precondition: pdf.NewReader rejected %s: %v", tc.name, err)
			}
			// Verify ntpdf's text-mode loader now rejects it.
			m := NewWithConfig(Config{Cols: 80, Rows: 24, Limits: tc.limits})
			cmd := m.SetPDFData(tc.name, tc.data)
			msg := cmd()
			if _, ok := msg.(pdfErrMsg); !ok {
				t.Fatalf("expected pdfErrMsg for %s, got %T", tc.name, msg)
			}
		})
	}
}
