package ntpdf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"runtime"
	"slices"
	"sync"
	"time"

	"nimbleterminal.dev/ntmediabox"
)

// DefaultWorkerTimeout bounds each worker open (including text extraction) or render.
const DefaultWorkerTimeout = 15 * time.Second

// WorkerConfig configures an OS-confined ntmediabox worker process for PDF page
// parsing, text extraction, and rasterization.
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
	// Timeout bounds each open or render operation. Non-positive values use DefaultWorkerTimeout.
	Timeout time.Duration
	// Limits bounds parsing and rendering. Also set Config.Limits for host file reads.
	// Negative values cannot disable wire or worker-side caps.
	Limits Limits
	// Start overrides startup and confinement; trusted callers must supply a closable
	// transport and a cleanup function that kills and reaps their worker.
	Start func(cfg WorkerConfig) (io.ReadWriteCloser, func() error, error)
}

type workerRenderer struct {
	mu              sync.Mutex
	client          *ntmediabox.RenderClient
	closeFn         func() error
	maxRenderPixels int
	maxRenderDPI    int
	timeout         time.Duration
	pages           []pdfPage
	stopped         bool
	closed          bool
}

// WorkerRendererFactory returns a RendererFactory that opens and rasterizes each
// PDF inside a separate OS-confined ntmediabox worker process, including text
// extraction. Worker failures fail widget loading without host parsing fallback.
func WorkerRendererFactory(cfg WorkerConfig) RendererFactory {
	cfg.Limits.applyDefaults()
	if cfg.Limits.MaxFileBytes <= 0 || cfg.Limits.MaxFileBytes > ntmediabox.MaxRenderDataBytes {
		cfg.Limits.MaxFileBytes = ntmediabox.MaxRenderDataBytes
	}
	if cfg.Limits.MaxRenderPixels <= 0 || cfg.Limits.MaxRenderPixels > ntmediabox.MaxRenderPixels {
		cfg.Limits.MaxRenderPixels = ntmediabox.MaxRenderPixels
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultWorkerTimeout
	}
	return func(name string, data []byte) (renderer Renderer, err error) {
		defer func() {
			if rec := recover(); rec != nil {
				err = fmt.Errorf("PDF worker panic: %v", rec)
			}
			if err != nil {
				err = &confinedLoadError{err}
			}
		}()
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

		opened := false
		defer func() {
			if !opened {
				_ = killAndClose()
			}
		}()
		client := ntmediabox.NewRenderClientWithKill(conn, func() {
			_ = killAndClose()
		})
		maxPixels := cfg.Limits.MaxRenderPixels
		if maxPixels <= 0 || maxPixels > ntmediabox.MaxRenderPixels {
			maxPixels = ntmediabox.MaxRenderPixels
		}
		ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
		defer cancel()
		options, err := json.Marshal(cfg.Limits)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(ctx, ntmediabox.RenderRequest{
			Meta:      options,
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
		pages, err := decodeWorkerPages(resp.Meta, cfg.Limits)
		if err != nil {
			return nil, err
		}
		if resp.Ended || resp.Pages != len(pages) {
			return nil, errors.New("invalid PDF worker open response")
		}
		opened = true
		return &workerRenderer{
			pages:           pages,
			timeout:         cfg.Timeout,
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
	if r.stopped {
		r.mu.Unlock()
		return nil, ErrRendererStopped
	}
	if r.closed || r.client == nil {
		r.mu.Unlock()
		return nil, errors.New("pdf worker renderer closed")
	}
	client := r.client
	r.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		r.stop()
		return nil, fmt.Errorf("RenderPageInDPI page=%d: %w", pageNum, err)
	}
	if pageNum < 1 || pageNum > len(r.pages) {
		return nil, fmt.Errorf("invalid page %d (1-indexed)", pageNum)
	}
	if dpi <= 0 {
		dpi = DefaultRenderDPI
	}
	if r.maxRenderDPI > 0 && dpi > r.maxRenderDPI {
		dpi = r.maxRenderDPI
	}
	resp, err := client.Do(ctx, ntmediabox.RenderRequest{
		Kind:      "pdf-render",
		Page:      pageNum,
		DPI:       dpi,
		MaxPixels: r.maxRenderPixels,
	})
	if err != nil {
		if ctx.Err() != nil {
			r.stop()
			return nil, fmt.Errorf("RenderPageInDPI page=%d: %w", pageNum, ctx.Err())
		}
		return nil, err
	}
	if resp.Ended || resp.Image == nil || resp.Page != pageNum {
		r.stop()
		return nil, fmt.Errorf("RenderPageInDPI page=%d: invalid worker response", pageNum)
	}
	return resp.Image, nil
}

func (r *workerRenderer) pdfPages() []pdfPage { return r.pages }

func (r *workerRenderer) stop() {
	r.mu.Lock()
	r.stopped = true
	r.mu.Unlock()
	_ = r.client.Close()
}

func (r *workerRenderer) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	client, closeFn := r.client, r.closeFn
	r.mu.Unlock()
	// Never hold the state lock across RPC: Close must interrupt a hung render.
	if client != nil {
		_ = client.Close()
	}
	if closeFn != nil {
		return closeFn()
	}
	return nil
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
		timeout: DefaultWorkerTimeout,
	}
	return ntmediabox.ServeRender(conn, h)
}

type pdfWorkerHandler struct {
	limits       Limits
	factory      RendererFactory
	timeout      time.Duration
	r            Renderer
	renderLimits Limits
	pages        int
}

func (h *pdfWorkerHandler) Handle(req ntmediabox.RenderRequest) (ntmediabox.RenderResponse, error) {
	switch req.Kind {
	case "pdf-open":
		if h.r != nil {
			_ = h.r.Close()
			h.r = nil
		}
		lim := h.limits
		if len(req.Meta) > 0 {
			var options Limits
			if len(req.Meta) > 1024 || json.Unmarshal(req.Meta, &options) != nil {
				return ntmediabox.RenderResponse{Ended: true}, errors.New("invalid PDF open limits")
			}
			lim = tighterWorkerLimits(lim, options)
		}
		if req.MaxPixels > 0 && (lim.MaxRenderPixels <= 0 || req.MaxPixels < lim.MaxRenderPixels) {
			lim.MaxRenderPixels = req.MaxPixels
		}
		if lim.MaxFileBytes > 0 && int64(len(req.Data)) > lim.MaxFileBytes {
			return ntmediabox.RenderResponse{Ended: true}, fmt.Errorf("pdf exceeds %d bytes", lim.MaxFileBytes)
		}
		pages, err := extractDocumentPages(req.Data, lim)
		if err != nil {
			return ntmediabox.RenderResponse{Ended: true}, err
		}
		meta, err := encodeWorkerPages(pages, lim)
		if err != nil {
			return ntmediabox.RenderResponse{Ended: true}, err
		}
		pageCount := len(pages)
		pages = nil
		var r Renderer
		if h.factory != nil {
			r, err = h.factory(req.Name, req.Data)
		} else {
			r, err = openRendererWithPages(req.Name, req.Data, nil, lim)
		}
		if err != nil {
			if r != nil {
				_ = r.Close()
			}
			return ntmediabox.RenderResponse{Ended: true}, err
		}
		if r == nil {
			return ntmediabox.RenderResponse{Ended: true}, errors.New("pdf renderer factory returned nil")
		}
		h.r = r
		h.renderLimits, h.pages = lim, pageCount
		return ntmediabox.RenderResponse{Meta: meta, Pages: pageCount}, nil

	case "pdf-render":
		if h.r == nil {
			return ntmediabox.RenderResponse{Ended: true}, errors.New("no PDF document open in worker")
		}
		if req.Page < 1 || req.Page > h.pages {
			return ntmediabox.RenderResponse{}, errors.New("invalid PDF page")
		}
		dpi := req.DPI
		if dpi <= 0 {
			dpi = DefaultRenderDPI
		}
		if h.renderLimits.MaxRenderDPI > 0 && dpi > h.renderLimits.MaxRenderDPI {
			dpi = h.renderLimits.MaxRenderDPI
		}
		timeout := h.timeout
		if timeout <= 0 {
			timeout = DefaultWorkerTimeout
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		img, err := RenderPageContext(ctx, h.r, req.Page, dpi)
		if err != nil {
			return ntmediabox.RenderResponse{}, err
		}
		rgba, err := checkedWorkerImage(img, req, h.renderLimits)
		if err != nil {
			return ntmediabox.RenderResponse{}, err
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
