package ntpdf

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
	"nimbleterminal.dev/ntmediabox"
)

// This capability survives withLimits without exposing parser objects to the host.
type pdfPagesProvider interface{ pdfPages() []pdfPage }

type confinedLoadError struct{ error }

func (e *confinedLoadError) Unwrap() error { return e.error }

const (
	workerMetadataVersion  = 1
	maxWorkerMetadataBytes = 8 << 20
	maxWorkerPages         = 10_000
	maxWorkerRuns          = 100_000
	maxWorkerImagesPerPage = 1024
)

type workerPage struct {
	Box    [4]float64 `json:"box"`
	Runs   []pdf.Text `json:"runs"`
	Images int        `json:"images"`
}

type workerMetadata struct {
	Version int               `json:"version"`
	Pages   []json.RawMessage `json:"pages"`
}

func validateWorkerPage(p workerPage, limits Limits, totalRuns *int) error {
	for _, v := range p.Box {
		if !boundedCoordinate(v) {
			return errors.New("invalid PDF page box")
		}
	}
	if p.Box[2] <= p.Box[0] || p.Box[3] <= p.Box[1] {
		return errors.New("invalid PDF page dimensions")
	}
	maxImages := maxWorkerImagesPerPage
	if limits.MaxImagesPerPage > 0 {
		maxImages = min(maxImages, limits.MaxImagesPerPage)
	}
	if p.Images < 0 || p.Images > maxImages {
		return errors.New("invalid PDF image count")
	}
	*totalRuns += len(p.Runs)
	if *totalRuns > maxWorkerRuns {
		return errors.New("PDF text run budget exceeded")
	}
	for _, r := range p.Runs {
		if !boundedCoordinate(r.X) || !boundedCoordinate(r.Y) || !boundedCoordinate(r.W) || !boundedCoordinate(r.FontSize) || len(r.Font) > 4096 || len(r.S) > 64<<10 || !utf8.ValidString(r.Font) || !utf8.ValidString(r.S) {
			return errors.New("invalid PDF text run")
		}
	}
	return nil
}

func boundedCoordinate(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && math.Abs(v) <= 1e9
}

func encodeWorkerPages(pages []pdfPage, limits Limits) ([]byte, error) {
	if len(pages) < 1 || len(pages) > maxWorkerPages {
		return nil, errors.New("PDF page metadata budget exceeded")
	}
	envelope := workerMetadata{Version: workerMetadataVersion}
	total, size := 0, 0
	for _, p := range pages {
		page := workerPage{Box: [4]float64{p.media.x0, p.media.y0, p.media.x1, p.media.y1}, Runs: p.runs, Images: p.imageCount}
		if err := validateWorkerPage(page, limits, &total); err != nil {
			return nil, err
		}
		data, err := json.Marshal(page)
		if err != nil {
			return nil, err
		}
		size += len(data) + 1
		if size > maxWorkerMetadataBytes-64 {
			return nil, errors.New("PDF metadata byte budget exceeded")
		}
		envelope.Pages = append(envelope.Pages, data)
	}
	return json.Marshal(envelope)
}

// Validate collection counts before allocating page or run slices. Raw metadata
// remains untrusted, even when the transport's length checks have succeeded.
func decodeWorkerPages(data []byte, limits Limits) ([]pdfPage, error) {
	if len(data) == 0 || len(data) > maxWorkerMetadataBytes || !utf8.Valid(data) {
		return nil, errors.New("invalid PDF worker metadata size or encoding")
	}
	maxPages := maxWorkerPages
	if limits.MaxPages > 0 {
		maxPages = min(maxPages, limits.MaxPages)
	}
	// Bound both page and text-run arrays before unmarshalling typed values.
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := checkMetadataTokens(dec, maxPages); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("trailing PDF metadata")
	}
	var envelope workerMetadata
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	if envelope.Version != workerMetadataVersion || len(envelope.Pages) < 1 || len(envelope.Pages) > maxPages {
		return nil, errors.New("invalid PDF worker metadata version or pages")
	}
	pages := make([]pdfPage, len(envelope.Pages))
	total := 0
	for i, raw := range envelope.Pages {
		var p workerPage
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		if err := validateWorkerPage(p, limits, &total); err != nil {
			return nil, err
		}
		pages[i] = pdfPage{media: mediaBox{p.Box[0], p.Box[1], p.Box[2], p.Box[3]}, runs: p.Runs, imageCount: p.Images}
	}
	return pages, nil
}

// The schema has only three array types: pages, runs, and four box coordinates.
// A conservative global token cap also bounds unknown fields and nested input.
func checkMetadataTokens(dec *json.Decoder, maxPages int) error {
	tokens, runs := 0, 0
	var value func(string, int) error
	value = func(name string, depth int) error {
		tokens++
		if depth > 8 || tokens > 2_000_000 {
			return errors.New("PDF metadata nesting or token budget exceeded")
		}
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			for dec.More() {
				key, err := dec.Token()
				if err != nil {
					return err
				}
				field, ok := key.(string)
				if !ok {
					return errors.New("invalid PDF metadata field")
				}
				// encoding/json matches field names using Unicode case folding.
				// Apply the same equivalence when enforcing array budgets.
				for _, canonical := range []string{"pages", "runs", "box"} {
					if strings.EqualFold(field, canonical) {
						field = canonical
						break
					}
				}
				if err := value(field, depth+1); err != nil {
					return err
				}
			}
		case '[':
			count := 0
			for dec.More() {
				count++
				if name == "pages" && count > maxPages {
					return errors.New("PDF page metadata budget exceeded")
				}
				if name == "box" && count > 4 {
					return errors.New("invalid PDF page box")
				}
				if name == "runs" {
					runs++
					if runs > maxWorkerRuns {
						return errors.New("PDF text run budget exceeded")
					}
				}
				if err := value("", depth+1); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid PDF metadata structure")
		}
		_, err = dec.Token()
		return err
	}
	return value("", 0)
}

func tighterWorkerLimits(a, b Limits) Limits {
	tighter := func(x, y int) int {
		if y > 0 && (x <= 0 || y < x) {
			return y
		}
		return x
	}
	tighter64 := func(x, y int64) int64 {
		if y > 0 && (x <= 0 || y < x) {
			return y
		}
		return x
	}
	a.MaxFileBytes = tighter64(a.MaxFileBytes, b.MaxFileBytes)
	a.MaxStreamBytes = tighter64(a.MaxStreamBytes, b.MaxStreamBytes)
	a.MaxInflatedBytes = tighter64(a.MaxInflatedBytes, b.MaxInflatedBytes)
	a.MaxPages = tighter(a.MaxPages, b.MaxPages)
	a.MaxImagesPerPage = tighter(a.MaxImagesPerPage, b.MaxImagesPerPage)
	a.MaxRenderDPI = tighter(a.MaxRenderDPI, b.MaxRenderDPI)
	a.MaxRenderPixels = tighter(a.MaxRenderPixels, b.MaxRenderPixels)
	return a
}

// Check bounds before ToRGBA can allocate a conversion buffer. The transport
// independently checks the response before allocating an image in the host.
func checkedWorkerImage(img image.Image, req ntmediabox.RenderRequest, limits Limits) (*image.RGBA, error) {
	if img == nil {
		return nil, errors.New("empty rendered PDF page")
	}
	bounds := img.Bounds()
	// Reject reversed bounds before subtraction: an overflowing negative span
	// can otherwise wrap into a small positive width or height.
	if bounds.Max.X <= bounds.Min.X || bounds.Max.Y <= bounds.Min.Y {
		return nil, errors.New("invalid rendered PDF bounds")
	}
	w, h := bounds.Dx(), bounds.Dy()
	maxW, maxH, maxPixels := ntmediabox.MaxRenderSide, ntmediabox.MaxRenderSide, ntmediabox.MaxRenderPixels
	if req.Width > 0 {
		maxW = min(maxW, req.Width)
	}
	if req.Height > 0 {
		maxH = min(maxH, req.Height)
	}
	if req.MaxPixels > 0 {
		maxPixels = min(maxPixels, req.MaxPixels)
	}
	if limits.MaxRenderPixels > 0 {
		maxPixels = min(maxPixels, limits.MaxRenderPixels)
	}
	if w <= 0 || h <= 0 || w > maxW || h > maxH || w > maxPixels/h {
		return nil, fmt.Errorf("rendered PDF %dx%d exceeds image budget", w, h)
	}
	return ntmediabox.ToRGBA(img), nil
}
