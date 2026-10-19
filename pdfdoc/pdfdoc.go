// Package pdfdoc provides a headless, resource-budgeted PDF reader backed by
// github.com/ledongthuc/pdf. It has no Bubble Tea or UI dependencies.
//
// Every Document is constructed via Open, which runs a strict, fail-closed
// lexical and stream-decoding preflight before handing the byte slice to
// pdf.NewReader:
//
//   - Input size and page count caps (MaxFileBytes, MaxPages).
//   - Declared stream lengths (including indirect integer lengths, rejecting
//     ambiguous or duplicate definitions) verified against endstream boundaries.
//   - Filter chains (FlateDecode, ASCII85Decode, ASCIIHexDecode,
//     RunLengthDecode, DCTDecode, CCITTFaxDecode) decoded through per-stream
//     (MaxStreamBytes) and whole-document (MaxInflatedBytes) byte budgets,
//     plus pixel caps (MaxImagePixels) for image streams and PNG predictors.
//   - Cross-reference tables, cross-reference streams, and object streams
//     checked so that startxref, /Prev, /XRefStm, and object offsets cannot
//     point inside strings, comments, or unvalidated streams.
//   - Cooperative deadline checks (Timeout / Deadline) during preflight
//     lexing and stream inflation.
//   - Bounded page-tree Parent walks (MaxTreeWalk) for MediaBox inheritance
//     and bounded content-stream / Form XObject walks (MaxFormDepth,
//     MaxImageDraws) for image counting.
//   - Synchronous panic recovery around all downstream github.com/ledongthuc/pdf
//     calls so library panics on malformed structures return ordinary errors.
//
// What this package does NOT guard against:
//
// A Go library cannot preempt an uncooperative CPU loop or unbounded
// allocation inside github.com/ledongthuc/pdf once preflight succeeds and the
// downstream parser is running in the same OS process. Hosts that handle
// untrusted PDFs and require hard wall-clock termination should invoke Open
// and page extraction inside a separate, killable worker process with an OS
// timeout (as gloss does).
package pdfdoc

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/ledongthuc/pdf"
)

// Default resource caps applied by Open when the corresponding Limits field is zero.
const (
	DefaultMaxFileBytes     int64         = 256 << 20        // 256 MiB input file size
	DefaultMaxPages         int           = 10_000           // maximum pages per document
	DefaultMaxStreamBytes   int64         = 256 << 20        // 256 MiB per decoded stream
	DefaultMaxInflatedBytes int64         = 1 << 30          // 1 GiB across all decoded streams
	DefaultMaxImagePixels   int64         = 100_000_000      // 100 MP per encoded image stream or predictor row
	DefaultParseTimeout     time.Duration = 15 * time.Second // cooperative preflight deadline
	DefaultMaxTreeWalk      int           = 64               // maximum Parent hops in page tree
	DefaultMaxFormDepth     int           = 8                // maximum nested Form XObject depth
	DefaultMaxImageDraws    int           = 100_000          // maximum image draws counted per page
)

// Limits configures the resource caps enforced by Open and Document methods.
// Zero-valued fields use the package defaults above. Setting a numeric or
// duration field to a negative value disables that cap (use only with trusted
// input).
type Limits struct {
	// MaxFileBytes caps the input PDF byte length. Zero = DefaultMaxFileBytes (256 MiB).
	MaxFileBytes int64

	// MaxPages caps the number of pages a PDF may report. Zero = DefaultMaxPages (10,000).
	MaxPages int

	// MaxStreamBytes caps how many bytes a single PDF stream may inflate to
	// during preflight. Zero = DefaultMaxStreamBytes (256 MiB).
	MaxStreamBytes int64

	// MaxInflatedBytes caps the total bytes all PDF streams in a document may
	// inflate to during preflight. Zero = DefaultMaxInflatedBytes (1 GiB).
	MaxInflatedBytes int64

	// MaxImagePixels caps the pixel count (width * height) of DCTDecode and
	// CCITTFaxDecode image streams and predictor Columns.
	// Zero = DefaultMaxImagePixels (100,000,000).
	MaxImagePixels int64

	// Timeout bounds cooperative syntax and stream preflight decoding when
	// Deadline is zero. Zero = DefaultParseTimeout (15s).
	Timeout time.Duration

	// Deadline is an optional absolute deadline for cooperative preflight
	// checks. When non-zero, it takes precedence over Timeout.
	Deadline time.Time

	// MaxTreeWalk caps the number of Parent links followed up the page tree
	// when resolving a page's MediaBox. Zero = DefaultMaxTreeWalk (64).
	MaxTreeWalk int

	// MaxFormDepth caps nested Form XObject traversal depth when counting
	// page images. Zero = DefaultMaxFormDepth (8).
	MaxFormDepth int

	// MaxImageDraws caps the number of image draw operations counted on a
	// single page. Zero = DefaultMaxImageDraws (100,000).
	MaxImageDraws int
}

func (l *Limits) applyDefaults() {
	if l.MaxFileBytes == 0 {
		l.MaxFileBytes = DefaultMaxFileBytes
	}
	if l.MaxPages == 0 {
		l.MaxPages = DefaultMaxPages
	}
	if l.MaxStreamBytes == 0 {
		l.MaxStreamBytes = DefaultMaxStreamBytes
	}
	if l.MaxInflatedBytes == 0 {
		l.MaxInflatedBytes = DefaultMaxInflatedBytes
	}
	if l.MaxImagePixels == 0 {
		l.MaxImagePixels = DefaultMaxImagePixels
	}
	if l.Timeout == 0 && l.Deadline.IsZero() {
		l.Timeout = DefaultParseTimeout
	}
	if l.MaxTreeWalk == 0 {
		l.MaxTreeWalk = DefaultMaxTreeWalk
	}
	if l.MaxFormDepth == 0 {
		l.MaxFormDepth = DefaultMaxFormDepth
	}
	if l.MaxImageDraws == 0 {
		l.MaxImageDraws = DefaultMaxImageDraws
	}
}

func (l Limits) maxStreamBytes() int64 {
	if l.MaxStreamBytes < 0 {
		return math.MaxInt64
	}
	if l.MaxStreamBytes == 0 {
		return DefaultMaxStreamBytes
	}
	return l.MaxStreamBytes
}

func (l Limits) maxInflatedBytes() int64 {
	if l.MaxInflatedBytes < 0 {
		return math.MaxInt64
	}
	if l.MaxInflatedBytes == 0 {
		return DefaultMaxInflatedBytes
	}
	return l.MaxInflatedBytes
}

func (l Limits) maxImagePixels() int64 {
	if l.MaxImagePixels < 0 {
		return math.MaxInt64
	}
	if l.MaxImagePixels == 0 {
		return DefaultMaxImagePixels
	}
	return l.MaxImagePixels
}

func (l Limits) deadline() time.Time {
	if !l.Deadline.IsZero() {
		return l.Deadline
	}
	if l.Timeout < 0 {
		return time.Now().Add(100 * 365 * 24 * time.Hour)
	}
	if l.Timeout == 0 {
		return time.Now().Add(DefaultParseTimeout)
	}
	return time.Now().Add(l.Timeout)
}

func (l Limits) maxTreeWalk() int {
	if l.MaxTreeWalk < 0 {
		return math.MaxInt
	}
	if l.MaxTreeWalk == 0 {
		return DefaultMaxTreeWalk
	}
	return l.MaxTreeWalk
}

func (l Limits) maxFormDepth() int {
	if l.MaxFormDepth < 0 {
		return math.MaxInt
	}
	if l.MaxFormDepth == 0 {
		return DefaultMaxFormDepth
	}
	return l.MaxFormDepth
}

func (l Limits) maxImageDraws() int {
	if l.MaxImageDraws < 0 {
		return math.MaxInt
	}
	if l.MaxImageDraws == 0 {
		return DefaultMaxImageDraws
	}
	return l.MaxImageDraws
}

// Box is a page rectangle in PDF points (1/72 inch).
type Box struct {
	X0, Y0, X1, Y1 float64
}

// Width returns the absolute width of the box in PDF points.
func (b Box) Width() float64 { return math.Abs(b.X1 - b.X0) }

// Height returns the absolute height of the box in PDF points.
func (b Box) Height() float64 { return math.Abs(b.Y1 - b.Y0) }

// Info holds document metadata read from the PDF trailer Info dictionary.
// CreationDate and ModDate are normalized via FormatDate.
type Info struct {
	Title        string
	Author       string
	Subject      string
	Keywords     string
	Creator      string
	Producer     string
	CreationDate string
	ModDate      string
}

// Document is a preflight-validated, open PDF reader. Construct via Open.
type Document struct {
	reader  *pdf.Reader
	limits  Limits
	pages   int
	version string
}

// Open validates data against limits (input size, stream/filter/predictor
// inflation budgets, and cross-reference boundary checks), opens the PDF
// reader, and verifies the page count. The budget check is mandatory: there is
// no separate unvalidated constructor.
func Open(data []byte, limits Limits) (*Document, error) {
	limits.applyDefaults()
	if len(data) == 0 {
		return nil, errors.New("empty pdf data")
	}
	if limits.MaxFileBytes > 0 && int64(len(data)) > limits.MaxFileBytes {
		return nil, fmt.Errorf("PDF is %d bytes, exceeds MaxFileBytes=%d", len(data), limits.MaxFileBytes)
	}
	if err := checkPDFStreams(data, limits); err != nil {
		return nil, err
	}
	type openResult struct {
		reader *pdf.Reader
		pages  int
	}
	res, err := recoverParse("opening PDF", func() (openResult, error) {
		r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return openResult{}, err
		}
		return openResult{reader: r, pages: r.NumPage()}, nil
	})
	if err != nil {
		return nil, err
	}
	if res.pages < 1 {
		return nil, errors.New("PDF reports zero pages")
	}
	if limits.MaxPages > 0 && res.pages > limits.MaxPages {
		return nil, fmt.Errorf("PDF page count %d exceeds MaxPages=%d (must be 1..%d)", res.pages, limits.MaxPages, limits.MaxPages)
	}
	return &Document{
		reader:  res.reader,
		limits:  limits,
		pages:   res.pages,
		version: HeaderVersion(data),
	}, nil
}

// NumPages returns the number of pages in the document (1..MaxPages).
func (d *Document) NumPages() int {
	return d.pages
}

// Version returns the PDF header version (for example, "1.6"), or "" if the
// header did not declare one.
func (d *Document) Version() string {
	return d.version
}

// Info reads the trailer Info dictionary strings. Malformed trailers or Info
// dictionaries recover cleanly and return whatever fields were readable.
func (d *Document) Info() Info {
	out, _ := recoverParse("reading PDF Info dictionary", func() (Info, error) {
		info := d.reader.Trailer().Key("Info")
		text := func(key string) string { return info.Key(key).Text() }
		return Info{
			Title:        text("Title"),
			Author:       text("Author"),
			Subject:      text("Subject"),
			Keywords:     text("Keywords"),
			Creator:      text("Creator"),
			Producer:     text("Producer"),
			CreationDate: FormatDate(text("CreationDate")),
			ModDate:      FormatDate(text("ModDate")),
		}, nil
	})
	return out
}

// MediaBox walks the page tree up to Limits.MaxTreeWalk levels to find the
// 1-indexed page's inherited MediaBox in PDF points. ok is false if no valid,
// finite, positive-area MediaBox is found.
func (d *Document) MediaBox(page int) (Box, bool, error) {
	if page < 1 || page > d.pages {
		return Box{}, false, nil
	}
	maxWalk := d.limits.maxTreeWalk()
	type boxResult struct {
		box Box
		ok  bool
	}
	res, err := recoverParse("reading PDF MediaBox", func() (boxResult, error) {
		for node, depth := d.reader.Page(page).V, 0; !node.IsNull() && depth < maxWalk; node, depth = node.Key("Parent"), depth+1 {
			box := node.Key("MediaBox")
			if box.Len() != 4 {
				continue
			}
			x0, y0 := box.Index(0).Float64(), box.Index(1).Float64()
			x1, y1 := box.Index(2).Float64(), box.Index(3).Float64()
			w, h := math.Abs(x1-x0), math.Abs(y1-y0)
			valid := w > 0 && h > 0 &&
				!math.IsNaN(w) && !math.IsNaN(h) && !math.IsInf(w, 0) && !math.IsInf(h, 0) &&
				!math.IsNaN(x0) && !math.IsNaN(y0) && !math.IsNaN(x1) && !math.IsNaN(y1) &&
				!math.IsInf(x0, 0) && !math.IsInf(y0, 0) && !math.IsInf(x1, 0) && !math.IsInf(y1, 0)
			if !valid {
				return boxResult{}, nil
			}
			return boxResult{box: Box{X0: x0, Y0: y0, X1: x1, Y1: y1}, ok: true}, nil
		}
		return boxResult{}, nil
	})
	return res.box, res.ok, err
}

// PageSize returns the 1-indexed page's MediaBox width and height in PDF
// points (1/72 inch). ok is false when the page declares no valid MediaBox.
func (d *Document) PageSize(page int) (widthPt, heightPt float64, ok bool, err error) {
	box, ok, err := d.MediaBox(page)
	if !ok || err != nil {
		return 0, 0, false, err
	}
	return box.Width(), box.Height(), true, nil
}

// PageText extracts the plain text of the 1-indexed page.
func (d *Document) PageText(page int) (string, error) {
	if page < 1 || page > d.pages {
		return "", fmt.Errorf("PDF page %d out of range 1..%d", page, d.pages)
	}
	return recoverParse("reading PDF page text", func() (string, error) {
		return d.reader.Page(page).GetPlainText(nil)
	})
}

// PageTextRuns extracts the positioned text runs of the 1-indexed page for
// spatial terminal layout.
func (d *Document) PageTextRuns(page int) ([]pdf.Text, error) {
	if page < 1 || page > d.pages {
		return nil, fmt.Errorf("PDF page %d out of range 1..%d", page, d.pages)
	}
	return recoverParse("reading PDF page text runs", func() ([]pdf.Text, error) {
		p := d.reader.Page(page)
		if p.V.IsNull() {
			return nil, nil
		}
		return p.Content().Text, nil
	})
}

// PageImages counts the images a 1-indexed page draws: image XObjects painted
// with Do, those inside Form XObjects up to Limits.MaxFormDepth, and inline BI
// images, capped at Limits.MaxImageDraws. An image drawn twice counts twice;
// an image only listed in Resources without being drawn does not count.
// Returns -1 when the content stream could not be interpreted.
func (d *Document) PageImages(page int) int {
	if page < 1 || page > d.pages {
		return 0
	}
	maxFormDepth := d.limits.maxFormDepth()
	maxImageDraws := d.limits.maxImageDraws()
	n, err := recoverParse("counting the PDF's images", func() (int, error) {
		p := d.reader.Page(page)
		if p.V.IsNull() {
			return 0, nil
		}
		count := 0
		var walk func(content, resources pdf.Value, depth int)
		walk = func(content, resources pdf.Value, depth int) {
			if content.IsNull() || depth > maxFormDepth {
				return
			}
			pdf.Interpret(content, func(stk *pdf.Stack, op string) {
				var operand pdf.Value
				for stk.Len() > 0 {
					operand = stk.Pop() // Do takes one; the last popped is the first pushed.
				}
				if count >= maxImageDraws {
					return
				}
				switch op {
				case "BI":
					count++
				case "Do":
					x := resources.Key("XObject").Key(operand.Name())
					switch x.Key("Subtype").Name() {
					case "Image":
						count++
					case "Form":
						inner := x.Key("Resources")
						if inner.IsNull() {
							inner = resources
						}
						walk(x, inner, depth+1)
					}
				}
			})
		}
		walk(p.V.Key("Contents"), p.Resources(), 0)
		return count, nil
	})
	if err != nil {
		return -1
	}
	return n
}

// HeaderVersion extracts the PDF version string from the "%PDF-x.y" header
// within the first 1 KiB of data, or returns "" if absent.
func HeaderVersion(data []byte) string {
	head := bytes.TrimSpace(data[:min(len(data), 1024)])
	if !bytes.HasPrefix(head, []byte("%PDF-")) {
		return ""
	}
	version := string(head[5:min(len(head), 16)])
	if i := strings.IndexFunc(version, func(r rune) bool { return r != '.' && !unicode.IsDigit(r) }); i >= 0 {
		version = version[:i]
	}
	return version
}

// FormatDate formats a PDF date string such as "D:20260304050607+02'00'", of
// which only the year is required, into a human-readable timestamp.
// Unrecognized strings are returned unchanged.
func FormatDate(s string) string {
	s = strings.TrimPrefix(strings.TrimSpace(s), "D:")
	digits := strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' })
	if digits < 0 {
		digits = len(s)
	}
	if digits < 4 {
		return s
	}
	zone := strings.NewReplacer("'", ":").Replace(strings.TrimSuffix(s[digits:], "'"))
	s = s[:min(digits, 14)]
	out := s[:4]
	for i, sep := range []string{"-", "-", " ", ":", ":"} {
		if len(s) < 6+2*i {
			return out
		}
		out += sep + s[4+2*i : 6+2*i]
	}
	switch {
	case strings.HasPrefix(zone, "Z"):
		out += " UTC"
	case len(zone) == 6:
		out += " " + zone
	}
	return out
}

// recoverParse turns library panics into errors synchronously so a timeout or
// caller return never leaves a background goroutine running.
func recoverParse[T any](what string, work func() (T, error)) (value T, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%s: %v", what, r)
		}
	}()
	return work()
}
