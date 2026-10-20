// jsrenderer_js.go: browser-WASM Renderer backed by
// ntshim/pdfium (the @embedpdf/pdfium JS bridge installed by
// `go tool ntshim-assets --shim=pdfium`).
//
// Conforms to the same ntpdf.Renderer contract as the native
// pdfium-via-wazero implementation. Mutex serialization, idempotent
// Close, and the GC leak guard live in pdfium.Document; this file is
// only the 1-indexed-page / DPI-budget adapter.

//go:build js && wasm

package ntpdf

import (
	"context"
	"fmt"
	"image"
	"math"
	"sync"

	"nimbleterminal.dev/ntshim/pdfium"
)

// jsRenderer adapts a pdfium.Document to the Renderer and ContextRenderer
// interfaces.
type jsRenderer struct {
	mu              sync.Mutex
	doc             *pdfium.Document
	maxRenderPixels int
	stopped         bool
}

// DefaultRendererFactory returns a RendererFactory that delegates to
// the ntshim/pdfium bridge. The first factory call (per process)
// initialises the JS bridge; subsequent calls just open new documents
// against the already-loaded PDFium runtime.
//
// If the host page hasn't loaded pdfium-shim.js, every factory
// invocation returns an error (pdfium.ErrBridgeMissing) and ImageMode
// degrades to TextMode.
func DefaultRendererFactory() RendererFactory {
	return DefaultRendererFactoryWithLimits(Limits{})
}

// DefaultRendererFactoryWithLimits is the same factory parameterised
// by resource caps. MaxRenderPixels is enforced inside the renderer
// (it queries page size via the bridge); MaxRenderDPI is enforced by
// the withLimits wrapper NewWithConfig applies upstream.
func DefaultRendererFactoryWithLimits(limits Limits) RendererFactory {
	limits.applyDefaults()
	return func(name string, data []byte) (Renderer, error) {
		if len(data) == 0 {
			return nil, fmt.Errorf("empty PDF data for %q", name)
		}
		doc, err := pdfium.Load(context.Background(), data)
		if err != nil {
			return nil, fmt.Errorf("ntshim/pdfium load %q: %w", name, err)
		}
		return &jsRenderer{doc: doc, maxRenderPixels: limits.MaxRenderPixels}, nil
	}
}

// RenderPage rasterizes a 1-indexed page. MaxRenderPixels enforcement:
// when the projected pixel count exceeds the budget, DPI is reduced to
// fit (graceful degradation matching the native implementation).
func (r *jsRenderer) RenderPage(pageNum, dpi int) (image.Image, error) {
	return r.RenderPageContext(context.Background(), pageNum, dpi)
}

// RenderPageContext rasterizes a 1-indexed page with context cancellation
// support. It passes ctx through to ntshim/pdfium's Document.RenderPage and
// selects on ctx.Done() (since ntshim v0.6.0's JS bridge runs @embedpdf/pdfium
// on the main JS thread and does not yet abort in-flight promises via ctx).
// When ctx is cancelled, the renderer is marked as stopped so subsequent calls
// return ErrRendererStopped, and the underlying Document is closed.
func (r *jsRenderer) RenderPageContext(ctx context.Context, pageNum, dpi int) (image.Image, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return nil, ErrRendererStopped
	}
	if err := ctx.Err(); err != nil {
		r.stopped = true
		r.mu.Unlock()
		_ = r.doc.Close()
		return nil, fmt.Errorf("ntshim/pdfium RenderPage page=%d: %w", pageNum, err)
	}
	r.mu.Unlock()

	if pageNum < 1 {
		return nil, fmt.Errorf("invalid page %d (1-indexed)", pageNum)
	}
	if dpi <= 0 {
		dpi = DefaultRenderDPI
	}
	if r.maxRenderPixels > 0 {
		dpi = r.clampDPIToBudget(pageNum, dpi)
	}
	if err := ctx.Err(); err != nil {
		r.mu.Lock()
		r.stopped = true
		r.mu.Unlock()
		_ = r.doc.Close()
		return nil, fmt.Errorf("ntshim/pdfium RenderPage page=%d: %w", pageNum, err)
	}

	type renderResult struct {
		img image.Image
		err error
	}
	ch := make(chan renderResult, 1)
	go func() {
		img, err := r.doc.RenderPage(ctx, pageNum-1, dpi)
		ch <- renderResult{img: img, err: err}
	}()

	select {
	case <-ctx.Done():
		r.mu.Lock()
		r.stopped = true
		r.mu.Unlock()
		go func() {
			<-ch
			_ = r.doc.Close()
		}()
		return nil, fmt.Errorf("ntshim/pdfium RenderPage page=%d: %w", pageNum, ctx.Err())
	case res := <-ch:
		if err := ctx.Err(); err != nil {
			r.mu.Lock()
			r.stopped = true
			r.mu.Unlock()
			_ = r.doc.Close()
			return nil, fmt.Errorf("ntshim/pdfium RenderPage page=%d: %w", pageNum, err)
		}
		return res.img, res.err
	}
}

// clampDPIToBudget queries the page's size in points and returns a DPI
// clamped so the rendered bitmap won't exceed r.maxRenderPixels.
// Returns the original DPI if page dims are unavailable — best-effort,
// mirroring native behavior.
func (r *jsRenderer) clampDPIToBudget(pageNum, dpi int) int {
	wPt, hPt, err := r.doc.PageSize(pageNum - 1)
	if err != nil || wPt <= 0 || hPt <= 0 {
		return dpi
	}
	wIn := wPt / 72.0
	hIn := hPt / 72.0
	projected := wIn * hIn * float64(dpi) * float64(dpi)
	if projected <= float64(r.maxRenderPixels) {
		return dpi
	}
	maxDPI := math.Sqrt(float64(r.maxRenderPixels) / (wIn * hIn))
	if maxDPI < 1 {
		maxDPI = 1
	}
	return int(maxDPI)
}

// Close releases the document. Idempotent; serialization against an
// in-flight RenderPage is handled inside pdfium.Document.
func (r *jsRenderer) Close() error { return r.doc.Close() }
