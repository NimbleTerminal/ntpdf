//go:build !wasm

package ntpdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// buildSlowRenderPDF constructs a 1-page PDF whose content stream draws thousands
// of overlapping full-page bezier paths so that rasterizing at 600 DPI in WASM
// takes multiple seconds unless interrupted by context cancellation.
func buildSlowRenderPDF() []byte {
	var stream bytes.Buffer
	stream.WriteString("0.5 w 0.2 0.4 0.8 RG 0.8 0.3 0.2 rg\n")
	unit := "0 0 m 612 200 0 600 612 792 c 0 792 l 612 0 l b\n"
	stream.WriteString(strings.Repeat(unit, 8000))
	body := stream.String()

	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(body), body),
	}

	var pdfBuf bytes.Buffer
	pdfBuf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, obj := range objects {
		offsets[i] = pdfBuf.Len()
		fmt.Fprintf(&pdfBuf, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := pdfBuf.Len()
	fmt.Fprintf(&pdfBuf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&pdfBuf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&pdfBuf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return pdfBuf.Bytes()
}

func TestRenderPageContextCancelBeforeAndDuringRender(t *testing.T) {
	path := repoTestdata(t, "Example.pdf")
	exampleData, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	slowData := buildSlowRenderPDF()

	// Disable MaxRenderPixels so the 600-DPI slow render isn't down-clamped.
	factory := DefaultRendererFactoryWithLimits(Limits{MaxRenderPixels: -1})

	// Hold 3 of the 4 pool instances open so that every subsequent open/cancel
	// must reuse the 4th pool slot. If any cancelled render fails to free its
	// pool instance, the next factory call will exhaust the pool.
	held := make([]Renderer, 3)
	for i := range held {
		r, err := factory(fmt.Sprintf("held-%d.pdf", i), exampleData)
		if err != nil {
			t.Fatalf("open held[%d]: %v", i, err)
		}
		held[i] = r
	}
	defer func() {
		for _, r := range held {
			if r != nil {
				_ = r.Close()
			}
		}
	}()

	// 1. Cancel BEFORE render starts.
	rBefore, err := factory("before.pdf", exampleData)
	if err != nil {
		t.Fatalf("open rBefore: %v", err)
	}
	defer rBefore.Close()

	preCtx, preCancel := context.WithCancel(context.Background())
	preCancel()

	start := time.Now()
	if _, err := RenderPageContext(preCtx, rBefore, 1, 72); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled RenderPageContext err = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Errorf("pre-cancelled RenderPageContext took %v, want prompt return", elapsed)
	}
	// Subsequent call on stopped renderer must fail plainly with ErrRendererStopped.
	if _, err := rBefore.RenderPage(1, 72); !errors.Is(err, ErrRendererStopped) {
		t.Fatalf("subsequent RenderPage after pre-cancel = %v, want ErrRendererStopped", err)
	}

	// 2. Cancel DURING render via beforeRender hook (without having called rBefore.Close()
	// yet — proving rBefore already freed the 4th pool slot).
	rHook, err := factory("hook.pdf", exampleData)
	if err != nil {
		t.Fatalf("open rHook (pool slot not freed after pre-cancel?): %v", err)
	}
	defer rHook.Close()

	prHook := rHook.(*pdfiumRenderer)
	entered := make(chan struct{})
	prHook.beforeRender = func(ctx context.Context) {
		close(entered)
		<-ctx.Done()
	}

	hookCtx, hookCancel := context.WithCancel(context.Background())
	go func() {
		<-entered
		time.Sleep(10 * time.Millisecond)
		hookCancel()
	}()

	start = time.Now()
	if _, err := prHook.RenderPageContext(hookCtx, 1, 72); !errors.Is(err, context.Canceled) {
		t.Fatalf("hook-cancelled RenderPageContext err = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("hook-cancelled RenderPageContext took %v, want prompt return", elapsed)
	}
	if _, err := prHook.RenderPage(1, 72); !errors.Is(err, ErrRendererStopped) {
		t.Fatalf("subsequent RenderPage after hook cancel = %v, want ErrRendererStopped", err)
	}

	// 3. Cancel DURING an expensive in-flight WASM FPDF_RenderPageBitmap draw.
	rSlow, err := factory("slow.pdf", slowData)
	if err != nil {
		t.Fatalf("open rSlow (pool slot not freed after hook cancel?): %v", err)
	}
	defer rSlow.Close()

	prSlow := rSlow.(*pdfiumRenderer)
	wasmStarted := make(chan struct{})
	prSlow.beforeRender = func(context.Context) {
		close(wasmStarted)
	}

	slowCtx, slowCancel := context.WithCancel(context.Background())
	go func() {
		<-wasmStarted
		// Give PDFium 40ms to enter the heavy FPDF_RenderPageBitmap loop inside WASM.
		time.Sleep(40 * time.Millisecond)
		slowCancel()
	}()

	start = time.Now()
	if _, err := RenderPageContext(slowCtx, rSlow, 1, 600); !errors.Is(err, context.Canceled) {
		t.Fatalf("in-flight WASM cancelled RenderPageContext err = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Errorf("in-flight WASM cancelled RenderPageContext took %v, want prompt interruption", elapsed)
	}
	if _, err := rSlow.RenderPage(1, 72); !errors.Is(err, ErrRendererStopped) {
		t.Fatalf("subsequent RenderPage after WASM cancel = %v, want ErrRendererStopped", err)
	}

	// 4. Verify a new renderer opens from the pool and renders cleanly afterwards
	// (even before rSlow.Close() is called).
	rAfter, err := factory("after.pdf", exampleData)
	if err != nil {
		t.Fatalf("open rAfter (pool slot not freed after WASM Kill?): %v", err)
	}
	defer rAfter.Close()

	img, err := RenderPageContext(context.Background(), rAfter, 1, 72)
	if err != nil {
		t.Fatalf("rAfter.RenderPageContext failed: %v", err)
	}
	if img == nil || img.Bounds().Empty() {
		t.Fatalf("rAfter.RenderPageContext returned empty image: %v", img)
	}
}
