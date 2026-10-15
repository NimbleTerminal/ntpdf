package pdfdoc

import (
	"strings"
	"testing"

	"nimbleterminal.dev/ntpdf"
)

func TestGeneratedPDFsParseAndRender(t *testing.T) {
	samples := GallerySamples(nil)
	_, repBar := BuildReport(false, 0, 0, 0)
	_, repLine := BuildReport(true, 1, 1, 2)
	samples = append(samples,
		Sample{Name: "report-bar.pdf", Data: repBar},
		Sample{Name: "report-line.pdf", Data: repLine},
		Sample{Name: "aspect.pdf", Data: BuildAspectSample()},
	)

	for _, s := range samples {
		t.Run(s.Name, func(t *testing.T) {
			m := ntpdf.NewWithConfig(ntpdf.Config{
				Cols:        80,
				Rows:        24,
				DefaultMode: ntpdf.TextMode,
				InitialData: s.Data,
				InitialName: s.Name,
				// Skip pdfium startup in fast unit test; test text extraction & structure first.
				RendererFactory: func(string, []byte) (ntpdf.Renderer, error) { return nil, nil },
			})
			cmd := m.Init()
			if cmd == nil {
				t.Fatal("Init returned nil")
			}
			// Run batch messages from Init
			msg := cmd()
			if batch, ok := msg.(interface{ Msgs() []any }); ok {
				_ = batch
			}
			// Also test via SetPDFData which returns the load cmd directly
			loadCmd := m.SetPDFData(s.Name, s.Data)
			m, _ = m.Update(loadCmd())
			if err := m.Err(); err != nil {
				t.Fatalf("load %s failed: %v", s.Name, err)
			}
			if m.NumPages() < 2 {
				t.Fatalf("%s: expected >= 2 pages, got %d", s.Name, m.NumPages())
			}
			view := m.View().Content
			if strings.TrimSpace(view) == "" || strings.Contains(view, "(empty)") {
				t.Fatalf("%s: expected non-empty TextMode view, got %q", s.Name, view)
			}
		})
	}
}
