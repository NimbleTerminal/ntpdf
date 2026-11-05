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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ledongthuc/pdf"
	"nimbleterminal.dev/ntmediabox"
)

func testWorkerMetadata(t *testing.T) []byte {
	t.Helper()
	data, err := encodeWorkerPages([]pdfPage{{media: defaultMediaBox(), runs: []pdf.Text{{X: 50, Y: 700, FontSize: 12, W: 50, S: "Worker text"}}}}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func responseWorkerFactory(t *testing.T, timeout time.Duration, handler ntmediabox.RenderFunc) (RendererFactory, *atomic.Int32) {
	t.Helper()
	killed := new(atomic.Int32)
	return WorkerRendererFactory(WorkerConfig{Timeout: timeout, Start: func(WorkerConfig) (io.ReadWriteCloser, func() error, error) {
		a, b := net.Pipe()
		go func() { defer b.Close(); _ = ntmediabox.ServeRender(b, handler) }()
		return a, func() error { killed.Add(1); return a.Close() }, nil
	}}), killed
}

func TestWorkerMetadataAvoidsHostParsing(t *testing.T) {
	meta := testWorkerMetadata(t)
	factory, killed := responseWorkerFactory(t, time.Second, func(ntmediabox.RenderRequest) (ntmediabox.RenderResponse, error) {
		return ntmediabox.RenderResponse{Pages: 1, Meta: meta}, nil
	})
	// Invalid source bytes prove that the host consumes metadata, rather than
	// parsing the PDF; withLimits must preserve this behavior in a real widget.
	m := NewWithConfig(Config{Cols: 80, Rows: 24, RendererFactory: factory})
	m = driveCmd(m, m.SetPDFData("worker.pdf", []byte("not a PDF")))
	if m.Err() != nil || m.NumPages() != 1 || !strings.Contains(m.TextView(80, 24), "Worker text") {
		t.Fatalf("confined load: %v, %q", m.Err(), m.TextView(80, 24))
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if killed.Load() != 1 {
		t.Fatalf("cleanup called %d times", killed.Load())
	}
}

func TestWorkerFailureCannotFallBackToHost(t *testing.T) {
	source, err := os.ReadFile(repoTestdata(t, "Example.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	for name, meta := range map[string][]byte{
		"missing": nil, "malformed": []byte(`{"version":1}`), "version": []byte(`{"version":99,"pages":[{"box":[0,0,1,1]}]}`),
		"images": []byte(`{"version":1,"pages":[{"box":[0,0,1,1],"images":999999}]}`),
		"box":    []byte(`{"version":1,"pages":[{"box":[0,0,0,1]}]}`),
	} {
		t.Run(name, func(t *testing.T) {
			factory, killed := responseWorkerFactory(t, time.Second, func(ntmediabox.RenderRequest) (ntmediabox.RenderResponse, error) {
				return ntmediabox.RenderResponse{Pages: 1, Meta: meta}, nil
			})
			if _, ok := runLoadFromBytes("valid.pdf", source, 1, factory, Limits{}).(pdfErrMsg); !ok {
				t.Fatal("worker failure fell back to host parsing")
			}
			if killed.Load() != 1 {
				t.Fatalf("cleanup called %d times", killed.Load())
			}
		})
	}
	factory := WorkerRendererFactory(WorkerConfig{Start: func(WorkerConfig) (io.ReadWriteCloser, func() error, error) { return nil, nil, os.ErrPermission }})
	if _, ok := runLoadFromBytes("valid.pdf", source, 1, factory, Limits{}).(pdfErrMsg); !ok {
		t.Fatal("startup failure fell back to host")
	}
}

func TestWorkerMetadataCollectionBounds(t *testing.T) {
	for _, data := range []string{
		`{"version":1,"pages":[` + strings.Repeat(`null,`, maxWorkerPages) + `null]}`,
		`{"version":1,"pages":[{"box":[0,0,1,1],"runs":[` + strings.Repeat(`null,`, maxWorkerRuns) + `null]}]}`,
		`{"version":1,"pages":[{"box":[0,0,1,1,2]}]}`,
		`{"version":1,"PAGES":[` + strings.Repeat(`null,`, maxWorkerPages) + `null]}`,
		`{"version":1,"pages":[{"box":[0,0,1,1],"runſ":[` + strings.Repeat(`null,`, maxWorkerRuns) + `null]}]}`,
		`{"version":1,"pages":[{"box":[0,0,1e300,1]}]}`,
		`{"version":1,"pages":[{"box":[0,0,1,1]}]} {}`,
	} {
		if _, err := decodeWorkerPages([]byte(data), Limits{}); err == nil {
			t.Fatal("accepted invalid metadata")
		}
	}
	if _, err := decodeWorkerPages(testWorkerMetadata(t), Limits{MaxPages: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerTimeoutsAndCloseInterruptRPC(t *testing.T) {
	for _, operation := range []string{"open", "render", "close"} {
		t.Run(operation, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			defer close(release)
			meta := testWorkerMetadata(t)
			timeout := 40 * time.Millisecond
			if operation == "close" {
				timeout = time.Minute
			}
			factory, killed := responseWorkerFactory(t, timeout, func(req ntmediabox.RenderRequest) (ntmediabox.RenderResponse, error) {
				if req.Kind == "pdf-open" && operation != "open" {
					return ntmediabox.RenderResponse{Pages: 1, Meta: meta}, nil
				}
				close(started)
				<-release
				return ntmediabox.RenderResponse{}, nil
			})
			if operation == "open" {
				_, err := factory("hung.pdf", []byte("source"))
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("open: %v", err)
				}
			} else {
				r, err := factory("hung.pdf", []byte("source"))
				if err != nil {
					t.Fatal(err)
				}
				defer r.Close()
				done := make(chan error, 1)
				go func() { _, err := r.RenderPage(1, 72); done <- err }()
				select {
				case <-started:
				case <-time.After(time.Second):
					t.Fatal("render did not start")
				}
				if operation == "close" {
					closed := make(chan error, 1)
					go func() { closed <- r.Close() }()
					select {
					case err := <-closed:
						if err != nil {
							t.Fatal(err)
						}
					case <-time.After(time.Second):
						t.Fatal("Close blocked behind RPC")
					}
				}
				select {
				case err := <-done:
					if err == nil {
						t.Fatal("blocked RPC succeeded")
					}
					if operation == "render" && !errors.Is(err, context.DeadlineExceeded) {
						t.Fatalf("render: %v", err)
					}
				case <-time.After(time.Second):
					t.Fatal("RPC was not interrupted")
				}
			}
			if killed.Load() != 1 {
				t.Fatalf("cleanup called %d times", killed.Load())
			}
		})
	}
}

// At must never be reached when the dimensions are over budget: converting
// this synthetic image would allocate/copy before the transport could reject it.
type budgetImage struct{ bounds image.Rectangle }

func (b budgetImage) Bounds() image.Rectangle { return b.bounds }
func (b budgetImage) ColorModel() color.Model { return color.RGBAModel }
func (b budgetImage) At(int, int) color.Color { panic("converted over-budget image") }

type imageWorkerRenderer struct {
	image image.Image
	dpi   int
}

func (r *imageWorkerRenderer) RenderPage(_, dpi int) (image.Image, error) {
	r.dpi = dpi
	return r.image, nil
}
func (r *imageWorkerRenderer) Close() error { return nil }

func TestWorkerSessionLimitsAndRecoverableImageBudget(t *testing.T) {
	source := buildSlowRenderPDF()
	r := &imageWorkerRenderer{image: budgetImage{image.Rect(0, 0, 3, 3)}}
	limits := Limits{}
	limits.applyDefaults()
	h := &pdfWorkerHandler{limits: limits, factory: func(string, []byte) (Renderer, error) { return r, nil }}
	defer h.Close()
	_, err := h.Handle(ntmediabox.RenderRequest{Kind: "pdf-open", Data: source, MaxPixels: 4, Meta: []byte(`{"MaxRenderDPI":80}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Handle(ntmediabox.RenderRequest{Kind: "pdf-render", Page: 1, DPI: 300, MaxPixels: 100}); err == nil {
		t.Fatal("session image budget ignored")
	}
	if r.dpi != 80 {
		t.Fatalf("DPI=%d", r.dpi)
	}
	r.image = image.NewRGBA(image.Rect(0, 0, 1, 1))
	if _, err := h.Handle(ntmediabox.RenderRequest{Kind: "pdf-render", Page: 1, DPI: 300}); err != nil {
		t.Fatalf("session lost after recoverable error: %v", err)
	}
	if _, err := h.Handle(ntmediabox.RenderRequest{Kind: "pdf-render", Page: 0}); err == nil {
		t.Fatal("invalid page accepted")
	}
}

func TestPDFReadEnforcesByteBudget(t *testing.T) {
	for _, tc := range []struct {
		source string
		fail   bool
	}{{"abc", false}, {"abcd", false}, {"abcde", true}} {
		data, err := readPDFBytes(strings.NewReader(tc.source), 4)
		if (err != nil) != tc.fail {
			t.Fatalf("read %q: %q, %v", tc.source, data, err)
		}
		if err == nil && string(data) != tc.source {
			t.Fatalf("read truncated %q", tc.source)
		}
	}
}

func FuzzDecodeWorkerPages(f *testing.F) {
	f.Add([]byte(`{"version":1,"pages":[{"box":[0,0,612,792]}]}`))
	f.Add([]byte(`{"version":99,"pages":null}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		pages, err := decodeWorkerPages(data, Limits{MaxPages: 10, MaxImagesPerPage: 4})
		if err == nil && (len(pages) == 0 || len(pages) > 10) {
			t.Fatalf("invalid decoded page count %d", len(pages))
		}
	})
}
