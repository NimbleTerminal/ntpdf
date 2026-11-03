package ntpdf

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"runtime"
	"slices"
	"sync"

	"nimbleterminal.dev/ntmediabox"
)

// WorkerConfig configures an OS-confined ntmediabox worker process for PDF page
// rasterization.
type WorkerConfig struct {
	// WorkerPath is the worker executable path. When empty, os.Executable() is used.
	WorkerPath string
	// WorkerArgs are arguments passed to WorkerPath to enter ServeWorker mode.
	WorkerArgs []string
	// StagePath is the Linux Landlock + seccomp stage binary. When empty on Linux,
	// WorkerRendererFactory checks for "ntconfine-stage" beside os.Executable() and
	// falls back to WorkerPath when StageArgs is non-empty.
	StagePath string
	// StageArgs are optional prefix arguments passed to StagePath before
	// ntmediabox.StageArg.
	StageArgs []string
	// AllowUnsandboxed permits running the worker in an isolated child process
	// without OS kernel confinement when Probe().Available is false.
	AllowUnsandboxed bool
	// Limits bounds file size, DPI, and per-page pixel count.
	Limits Limits
	// Start overrides process startup (used by tests or custom transports).
	Start func(cfg WorkerConfig) (io.ReadWriteCloser, func() error, error)
}

type workerRenderer struct {
	mu              sync.Mutex
	client          *ntmediabox.RenderClient
	closeFn         func() error
	maxRenderPixels int
	maxRenderDPI    int
	stopped         bool
	closed          bool
}

// WorkerRendererFactory returns a RendererFactory that opens and rasterizes each
// PDF inside a separate OS-confined ntmediabox worker process.
func WorkerRendererFactory(cfg WorkerConfig) RendererFactory {
	cfg.Limits.applyDefaults()
	return func(name string, data []byte) (Renderer, error) {
		if len(data) == 0 {
			return nil, fmt.Errorf("pdf worker OpenDocument %q: empty data", name)
		}
		if cfg.Limits.MaxFileBytes > 0 && int64(len(data)) > cfg.Limits.MaxFileBytes {
			return nil, fmt.Errorf("pdf worker OpenDocument %q: %d bytes exceeds limit %d", name, len(data), cfg.Limits.MaxFileBytes)
		}
		if len(data) > ntmediabox.MaxRenderDataBytes {
			return nil, fmt.Errorf("pdf worker OpenDocument %q: %d bytes exceeds wire limit %d", name, len(data), ntmediabox.MaxRenderDataBytes)
		}

		startFn := cfg.Start
		if startFn == nil {
			startFn = defaultStartPDFWorker
		}
		conn, closeFn, err := startFn(cfg)
		if err != nil {
			return nil, err
		}
		var closeOnce sync.Once
		killAndClose := func() error {
			var cerr error
			closeOnce.Do(func() {
				_ = conn.Close()
				if closeFn != nil {
					cerr = closeFn()
				}
			})
			return cerr
		}

		client := ntmediabox.NewRenderClientWithKill(conn, func() {
			_ = killAndClose()
		})
		maxPixels := cfg.Limits.MaxRenderPixels
		if maxPixels <= 0 || maxPixels > ntmediabox.MaxRenderPixels {
			maxPixels = ntmediabox.MaxRenderPixels
		}
		_, err = client.Do(context.Background(), ntmediabox.RenderRequest{
			Kind:      "pdf-open",
			Name:      name,
			MaxPixels: maxPixels,
			Data:      data,
		})
		if err != nil {
			_ = client.Close()
			_ = killAndClose()
			return nil, err
		}
		return &workerRenderer{
			client:          client,
			closeFn:         killAndClose,
			maxRenderPixels: maxPixels,
			maxRenderDPI:    cfg.Limits.MaxRenderDPI,
		}, nil
	}
}

func defaultStartPDFWorker(cfg WorkerConfig) (io.ReadWriteCloser, func() error, error) {
	workerPath := cfg.WorkerPath
	if workerPath == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, nil, err
		}
		workerPath = exe
	}
	resolvedWorker, err := ntmediabox.VerifyBinary(workerPath)
	if err != nil {
		return nil, nil, err
	}
	stagePath := cfg.StagePath
	stageArgs := slices.Clone(cfg.StageArgs)
	if runtime.GOOS == "linux" && stagePath == "" {
		if sib, sibErr := ntmediabox.ResolveSibling(ntmediabox.DefaultStageBinary); sibErr == nil {
			stagePath = sib
		} else if len(stageArgs) > 0 {
			stagePath = resolvedWorker
		}
	}
	execPaths := append(slices.Clone(ntmediabox.DefaultExecPaths()), resolvedWorker)
	h, err := ntmediabox.StartHelper(ntmediabox.HelperConfig{
		WorkerPath: resolvedWorker,
		WorkerArgs: slices.Clone(cfg.WorkerArgs),
		StagePath:  stagePath,
		StageArgs:  stageArgs,
		Policy: ntmediabox.Policy{
			ReadPaths:        ntmediabox.DefaultLibPaths(),
			ExecPaths:        execPaths,
			AllowUnsandboxed: cfg.AllowUnsandboxed,
		},
	})
	if err != nil {
		return nil, nil, err
	}
	return h.Conn, h.Close, nil
}

func (r *workerRenderer) RenderPage(pageNum, dpi int) (image.Image, error) {
	return r.RenderPageContext(context.Background(), pageNum, dpi)
}

func (r *workerRenderer) RenderPageContext(ctx context.Context, pageNum, dpi int) (image.Image, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return nil, ErrRendererStopped
	}
	if r.closed || r.client == nil {
		return nil, errors.New("pdf worker renderer closed")
	}
	if err := ctx.Err(); err != nil {
		r.stopped = true
		_ = r.client.Close()
		if r.closeFn != nil {
			_ = r.closeFn()
		}
		return nil, fmt.Errorf("RenderPageInDPI page=%d: %w", pageNum, err)
	}
	if pageNum < 1 {
		return nil, fmt.Errorf("invalid page %d (1-indexed)", pageNum)
	}
	if dpi <= 0 {
		dpi = DefaultRenderDPI
	}
	if r.maxRenderDPI > 0 && dpi > r.maxRenderDPI {
		dpi = r.maxRenderDPI
	}
	resp, err := r.client.Do(ctx, ntmediabox.RenderRequest{
		Kind:      "pdf-render",
		Page:      pageNum,
		DPI:       dpi,
		MaxPixels: r.maxRenderPixels,
	})
	if err != nil {
		if ctx.Err() != nil {
			r.stopped = true
			_ = r.client.Close()
			if r.closeFn != nil {
				_ = r.closeFn()
			}
			return nil, fmt.Errorf("RenderPageInDPI page=%d: %w", pageNum, ctx.Err())
		}
		return nil, err
	}
	if resp.Image == nil {
		return nil, fmt.Errorf("RenderPageInDPI page=%d: worker returned nil image", pageNum)
	}
	return resp.Image, nil
}

func (r *workerRenderer) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	var err error
	if r.client != nil {
		_ = r.client.Close()
	}
	if r.closeFn != nil {
		err = r.closeFn()
	}
	return err
}

// ServeWorker runs the confined worker side of a PDF renderer over conn using
// DefaultRendererFactoryWithLimits(limits).
func ServeWorker(conn io.ReadWriter, limits Limits) error {
	return ServeWorkerWithFactory(conn, limits, nil)
}

// ServeWorkerWithFactory runs the confined worker side of a PDF renderer over conn.
// When factory is nil, DefaultRendererFactoryWithLimits is used.
func ServeWorkerWithFactory(conn io.ReadWriter, limits Limits, factory RendererFactory) error {
	limits.applyDefaults()
	h := &pdfWorkerHandler{
		limits:  limits,
		factory: factory,
	}
	return ntmediabox.ServeRender(conn, h)
}

type pdfWorkerHandler struct {
	limits  Limits
	factory RendererFactory
	r       Renderer
}

func (h *pdfWorkerHandler) Handle(req ntmediabox.RenderRequest) (ntmediabox.RenderResponse, error) {
	switch req.Kind {
	case "pdf-open":
		if h.r != nil {
			_ = h.r.Close()
			h.r = nil
		}
		lim := h.limits
		if req.MaxPixels > 0 && (lim.MaxRenderPixels <= 0 || req.MaxPixels < lim.MaxRenderPixels) {
			lim.MaxRenderPixels = req.MaxPixels
		}
		if lim.MaxFileBytes > 0 && int64(len(req.Data)) > lim.MaxFileBytes {
			return ntmediabox.RenderResponse{Ended: true}, fmt.Errorf("pdf exceeds %d bytes", lim.MaxFileBytes)
		}
		fn := h.factory
		if fn == nil {
			fn = DefaultRendererFactoryWithLimits(lim)
		}
		if fn == nil {
			return ntmediabox.RenderResponse{Ended: true}, errors.New("pdf renderer unavailable on this target")
		}
		r, err := fn(req.Name, req.Data)
		if err != nil {
			return ntmediabox.RenderResponse{Ended: true}, err
		}
		if r == nil {
			return ntmediabox.RenderResponse{Ended: true}, errors.New("pdf renderer factory returned nil")
		}
		h.r = r
		return ntmediabox.RenderResponse{}, nil

	case "pdf-render":
		if h.r == nil {
			return ntmediabox.RenderResponse{Ended: true}, errors.New("no PDF document open in worker")
		}
		dpi := req.DPI
		if dpi <= 0 {
			dpi = DefaultRenderDPI
		}
		if h.limits.MaxRenderDPI > 0 && dpi > h.limits.MaxRenderDPI {
			dpi = h.limits.MaxRenderDPI
		}
		img, err := RenderPageContext(context.Background(), h.r, req.Page, dpi)
		if err != nil {
			return ntmediabox.RenderResponse{}, err
		}
		rgba := ntmediabox.ToRGBA(img)
		if rgba == nil {
			return ntmediabox.RenderResponse{}, errors.New("empty rendered page")
		}
		return ntmediabox.RenderResponse{
			Page:  req.Page,
			Image: rgba,
		}, nil

	default:
		return ntmediabox.RenderResponse{Ended: true}, fmt.Errorf("unknown pdf worker request %q", req.Kind)
	}
}

func (h *pdfWorkerHandler) Close() error {
	if h.r != nil {
		err := h.r.Close()
		h.r = nil
		return err
	}
	return nil
}
