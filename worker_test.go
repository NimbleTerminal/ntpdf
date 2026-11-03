//go:build !js && !wasm

package ntpdf

import (
	"context"
	"errors"
	"image"
	"image/color"
	"io"
	"net"
	"os"
	"runtime"
	"testing"
	"time"

	"nimbleterminal.dev/ntmediabox"
)

func init() {
	if len(os.Args) >= 2 && os.Args[1] == "--ntpdf-test-worker" {
		conn := ntmediabox.WorkerConn()
		defer conn.Close()
		if err := ServeWorkerWithFactory(conn, Limits{}, func(name string, data []byte) (Renderer, error) {
			return &stubWorkerPDFRenderer{}, nil
		}); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
}

type stubWorkerPDFRenderer struct {
	hang bool
}

func (s *stubWorkerPDFRenderer) RenderPage(pageNum, dpi int) (image.Image, error) {
	if s.hang {
		time.Sleep(5 * time.Second)
	}
	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	img.SetRGBA(0, 0, color.RGBA{R: uint8(pageNum), G: uint8(dpi & 0xff), B: 42, A: 255})
	return img, nil
}

func (s *stubWorkerPDFRenderer) Close() error { return nil }

func TestWorkerRendererInMemoryPipeAndCancel(t *testing.T) {
	hangNext := false
	factory := WorkerRendererFactory(WorkerConfig{
		Start: func(cfg WorkerConfig) (io.ReadWriteCloser, func() error, error) {
			a, b := net.Pipe()
			go ServeWorkerWithFactory(b, cfg.Limits, func(name string, data []byte) (Renderer, error) {
				return &stubWorkerPDFRenderer{hang: hangNext}, nil
			})
			return a, func() error { return a.Close() }, nil
		},
	})

	r, err := factory("doc.pdf", []byte("%PDF-1.4 stub"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	img, err := RenderPageContext(context.Background(), r, 2, 144)
	if err != nil {
		t.Fatal(err)
	}
	rgba, ok := img.(*image.RGBA)
	if !ok || rgba.Bounds() != image.Rect(0, 0, 8, 6) || rgba.RGBAAt(0, 0) != (color.RGBA{R: 2, G: 144, B: 42, A: 255}) {
		t.Fatalf("unexpected rendered page: %T %v %v", img, img.Bounds(), rgba.RGBAAt(0, 0))
	}

	// Cancel a render and verify ErrRendererStopped on subsequent calls.
	_ = r.Close()
	hangNext = true
	r2, err := factory("doc.pdf", []byte("%PDF-1.4 stub"))
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = RenderPageContext(ctx, r2, 1, 72)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
	if _, err := RenderPageContext(context.Background(), r2, 1, 72); !errors.Is(err, ErrRendererStopped) {
		t.Fatalf("expected ErrRendererStopped after cancel, got %v", err)
	}
}

func TestWorkerRendererSandboxedSubprocess(t *testing.T) {
	sup := ntmediabox.Probe()
	if !sup.Available && runtime.GOOS != "windows" {
		t.Skipf("sandbox unavailable: %s", sup.Detail)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg := WorkerConfig{
		WorkerPath:       exe,
		WorkerArgs:       []string{"--ntpdf-test-worker"},
		AllowUnsandboxed: !sup.Available,
	}
	if runtime.GOOS == "linux" {
		// On Linux in tests, if ntconfine-stage isn't built beside test binary, use in-memory pipe or stage if pure Go.
		if _, err := ntmediabox.ResolveSibling(ntmediabox.DefaultStageBinary); err != nil {
			t.Skip("ntconfine-stage not beside test binary on Linux")
		}
	}
	factory := WorkerRendererFactory(cfg)
	r, err := factory("sandboxed.pdf", []byte("%PDF-1.4 sandboxed"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	img, err := r.RenderPage(3, 96)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds() != image.Rect(0, 0, 8, 6) {
		t.Fatalf("unexpected bounds: %v", img.Bounds())
	}
}
