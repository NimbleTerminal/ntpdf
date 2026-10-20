// image.go: ImageMode plumbing.
//
// The widget owns a per-document Renderer whose lifetime is tied to the
// loaded PDF — SetPDF closes the previous one and opens a new one via the
// configured RendererFactory. Concrete factories (pdfium WASM today,
// future swap-ins) live in build-tagged sibling files so the browser-WASM
// build stays free of host-dependent code.

package ntpdf

import (
	"context"
	"errors"
	"image"
)

// ErrRendererStopped is returned when RenderPage or RenderPageContext is
// called on a Renderer whose previous render was cancelled via its context.
// Stopping an in-flight native PDFium render terminates the underlying WASM
// worker instance, leaving the renderer unusable; callers should close the
// stopped renderer and open a new one via the RendererFactory.
var ErrRendererStopped = errors.New("renderer stopped")

// Renderer rasterizes pages of a single loaded PDF document. Implementations
// are constructed via a RendererFactory (which opens the document) and
// released via Close.
//
// pageNum is 1-indexed to match the public Model API.
type Renderer interface {
	RenderPage(pageNum, dpi int) (image.Image, error)
	Close() error
}

// ContextRenderer is implemented by Renderers that support context-based
// cancellation of page rasterization, including the default native and
// browser-WASM renderers and the Limits wrapper.
//
// When ctx is cancelled before or during a render, RenderPageContext returns
// an error wrapping ctx.Err() and marks the renderer as stopped so subsequent
// calls fail with ErrRendererStopped.
type ContextRenderer interface {
	Renderer
	RenderPageContext(ctx context.Context, pageNum, dpi int) (image.Image, error)
}

// RenderPageContext rasterizes a 1-indexed page at dpi with cancellation
// support. When r implements ContextRenderer, its RenderPageContext method is
// invoked directly; otherwise ctx.Err() is checked before falling back to
// r.RenderPage(pageNum, dpi).
func RenderPageContext(ctx context.Context, r Renderer, pageNum, dpi int) (image.Image, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if r == nil {
		return nil, errors.New("nil renderer")
	}
	if cr, ok := r.(ContextRenderer); ok {
		return cr.RenderPageContext(ctx, pageNum, dpi)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.RenderPage(pageNum, dpi)
}

// RendererFactory opens a PDF from raw bytes and returns a Renderer
// bound to it. name is a human-readable label (typically the filename
// or "embedded"), used for diagnostics; it is not parsed.
//
// The widget invokes the factory on every SetPDF / SetPDFData call.
// For path-based loads the widget reads the file once and passes the
// bytes here, so factories never have to do their own I/O. A nil
// return value is valid when the platform has no renderer available
// (e.g. js/wasm), causing ImageMode to degrade to TextMode.
type RendererFactory func(name string, data []byte) (Renderer, error)

// pageRenderedMsg is delivered when a Renderer.RenderPage call succeeds.
// gen / loadGen are checked against the Model's current counters in
// Update to drop stale results — gen alone suffices today (SetPDF bumps
// renderGen), but tagging with loadGen too is defense-in-depth: it
// guarantees that a render produced by a previous document's Renderer
// can never be accepted for the current document even if the gen
// counters happen to wrap.
type pageRenderedMsg struct {
	page    int
	img     image.Image
	dpi     int
	gen     uint64
	loadGen uint64
}

// pageRenderErrMsg is delivered when a Renderer.RenderPage call fails.
type pageRenderErrMsg struct {
	page    int
	err     error
	gen     uint64
	loadGen uint64
}
