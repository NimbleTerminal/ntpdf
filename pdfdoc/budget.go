package pdfdoc

// This preflight is deliberately stricter than a repairing PDF reader. It
// follows stream lengths and filter arrays, and fails closed on constructs it
// cannot budget. It never asks the PDF library to allocate a decoded stream.
import (
	"bytes"
	"compress/zlib"
	"encoding/ascii85"
	"encoding/hex"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type pdfName string
type pdfString struct{}
type pdfRef struct{ id, gen int64 }
type pdfDict map[string]any
type pdfSyntax struct {
	data        []byte
	pos, tokens int
	until       time.Time
}

func pdfSpace(c byte) bool { return c == 0 || c == 9 || c == 10 || c == 12 || c == 13 || c == 32 }
func pdfDelim(c byte) bool { return pdfSpace(c) || strings.ContainsRune("()<>[]{}/%", rune(c)) }
func (p *pdfSyntax) skip() {
	for p.pos < len(p.data) {
		if pdfSpace(p.data[p.pos]) {
			p.pos++
			continue
		}
		if p.data[p.pos] == '%' {
			for p.pos < len(p.data) && p.data[p.pos] != '\n' && p.data[p.pos] != '\r' {
				p.pos++
			}
			continue
		}
		break
	}
}
func (p *pdfSyntax) value(depth int) (any, error) {
	p.skip()
	p.tokens++
	if depth > 64 || p.tokens > 1<<20 || time.Now().After(p.until) {
		return nil, errors.New("PDF syntax work limit exceeded")
	}
	if p.pos == len(p.data) {
		return nil, io.EOF
	}
	start := p.pos
	c := p.data[p.pos]
	p.pos++
	switch c {
	case '(':
		level := 1
		for p.pos < len(p.data) && level > 0 {
			c = p.data[p.pos]
			p.pos++
			switch c {
			case '\\':
				if p.pos < len(p.data) {
					p.pos++
				}
			case '(':
				level++
			case ')':
				level--
			}
			if level > 64 {
				return nil, errors.New("PDF string nesting exceeds limit")
			}
		}
		if level != 0 {
			return nil, io.ErrUnexpectedEOF
		}
		return pdfString{}, nil
	case '<':
		if p.pos < len(p.data) && p.data[p.pos] == '<' {
			p.pos++
			d := pdfDict{}
			for {
				p.skip()
				if bytes.HasPrefix(p.data[p.pos:], []byte(">>")) {
					p.pos += 2
					if err := pdfDictionaryBudget(d); err != nil {
						return nil, err
					}
					return d, nil
				}
				k, err := p.value(depth + 1)
				if err != nil {
					return nil, err
				}
				key, ok := k.(pdfName)
				if !ok {
					return nil, errors.New("invalid PDF dictionary key")
				}
				v, err := p.value(depth + 1)
				if err != nil {
					return nil, err
				}
				if _, duplicate := d[string(key)]; duplicate {
					return nil, errors.New("ambiguous duplicate PDF dictionary key")
				}
				d[string(key)] = v
			}
		}
		for p.pos < len(p.data) && p.data[p.pos] != '>' {
			p.pos++
		}
		if p.pos == len(p.data) {
			return nil, io.ErrUnexpectedEOF
		}
		p.pos++
		return pdfString{}, nil
	case '[':
		var a []any
		for {
			p.skip()
			if p.pos < len(p.data) && p.data[p.pos] == ']' {
				p.pos++
				return a, nil
			}
			v, err := p.value(depth + 1)
			if err != nil {
				return nil, err
			}
			a = append(a, v)
		}
	case '/':
		start = p.pos
		for p.pos < len(p.data) && !pdfDelim(p.data[p.pos]) {
			p.pos++
		}
		raw := p.data[start:p.pos]
		var out strings.Builder
		for i := 0; i < len(raw); i++ {
			if raw[i] == '#' {
				if i+2 >= len(raw) {
					return nil, errors.New("invalid escaped PDF name")
				}
				b, err := hex.DecodeString(string(raw[i+1 : i+3]))
				if err != nil {
					return nil, err
				}
				out.WriteByte(b[0])
				i += 2
			} else {
				out.WriteByte(raw[i])
			}
		}
		return pdfName(out.String()), nil
	}
	for p.pos < len(p.data) && !pdfDelim(p.data[p.pos]) {
		p.pos++
	}
	word := string(p.data[start:p.pos])
	if n, err := strconv.ParseInt(word, 10, 64); err == nil {
		// Look ahead for an indirect reference, without consuming ordinary numbers.
		saved := p.pos
		p.skip()
		s := p.pos
		for p.pos < len(p.data) && !pdfDelim(p.data[p.pos]) {
			p.pos++
		}
		gen, e := strconv.ParseInt(string(p.data[s:p.pos]), 10, 64)
		p.skip()
		if e == nil && p.pos < len(p.data) && p.data[p.pos] == 'R' && (p.pos+1 == len(p.data) || pdfDelim(p.data[p.pos+1])) {
			p.pos++
			return pdfRef{n, gen}, nil
		}
		p.pos = saved
		return n, nil
	}
	return word, nil
}

// Indirect integer lengths are common. Ambiguous definitions are rejected:
// preflight must not select a different value than the downstream xref reader.
var pdfFinalXref = regexp.MustCompile(`startxref[\x00\t\n\f\r ]+[0-9]+[\x00\t\n\f\r ]+%%EOF[\x00\t\n\f\r ]*$`)

var pdfIntegerObject = regexp.MustCompile(`(?m)(?:^|[\r\n])[ \t]*([0-9]+)[ \t]+([0-9]+)[ \t]+obj[\x00\t\n\f\r ]+([0-9]+)[\x00\t\n\f\r ]+endobj`)

type pdfStreamBudget struct {
	objects        map[int64]pdfRef
	references     []pdfXrefEntry
	objectStreams  map[int64][]int64
	current        pdfRef
	currentOffset  int64
	xrefOffsets    map[int64]bool
	xrefTargets    []int64
	lastStartXref  int
	actualIntegers map[pdfRef]int64
	usedIntegers   map[pdfRef]bool

	total, maxStream, maxTotal, maxPixels int64
	until                                 time.Time
	integers                              map[pdfRef]int64
}

func checkPDFStreams(data []byte, limits Limits) error {
	_, err := recoverParse("PDF stream preflight", func() (bool, error) { return true, checkPDFStreamSyntax(data, limits) })
	return err
}

func checkPDFStreamSyntax(data []byte, limits Limits) error {
	b := &pdfStreamBudget{
		maxStream: limits.maxStreamBytes(),
		maxTotal:  limits.maxInflatedBytes(),
		maxPixels: limits.maxImagePixels(),
		until:     limits.deadline(),
		integers:  map[pdfRef]int64{},
	}
	matches := pdfIntegerObject.FindAllSubmatch(data, 65537)
	if len(matches) > 65536 {
		return errors.New("PDF has too many indirect integers")
	}
	for _, m := range matches {
		id, e1 := strconv.ParseInt(string(m[1]), 10, 64)
		gen, e2 := strconv.ParseInt(string(m[2]), 10, 64)
		n, e3 := strconv.ParseInt(string(m[3]), 10, 64)
		if e1 != nil || e2 != nil || e3 != nil {
			return errors.New("invalid PDF integer")
		}
		key := pdfRef{id, gen}
		if old, ok := b.integers[key]; ok && old != n {
			return errors.New("ambiguous PDF stream length")
		}
		b.integers[key] = n
	}
	if err := b.scan(data, 0); err != nil {
		return err
	}
	if bytes.HasPrefix(data, []byte("%PDF-")) {
		match := pdfFinalXref.FindIndex(data)
		if match == nil || match[0] != b.lastStartXref {
			return errors.New("PDF final cross-reference is not at a validated boundary")
		}
	}
	return b.checkReferences()
}

func (b *pdfStreamBudget) integer(v any) (int64, bool) {
	if r, ok := v.(pdfRef); ok {
		n, found := b.integers[r]
		if b.usedIntegers == nil {
			b.usedIntegers = map[pdfRef]bool{}
		}
		b.usedIntegers[r] = true
		return n, found
	}
	n, ok := v.(int64)
	return n, ok
}

func (b *pdfStreamBudget) scan(data []byte, depth int) error {
	if depth > 8 {
		return errors.New("PDF object streams nest too deeply")
	}
	if b.objects == nil {
		b.objects = map[int64]pdfRef{}
		b.objectStreams = map[int64][]int64{}
		b.actualIntegers = map[pdfRef]int64{}
		b.xrefOffsets = map[int64]bool{}
		b.lastStartXref = -1
	}
	if b.maxPixels == 0 {
		b.maxPixels = DefaultMaxImagePixels
	}
	p := &pdfSyntax{data: data, until: b.until}
	var previous any
	expectValue := false
	activeObject := false
	strict := bytes.HasPrefix(data, []byte("%PDF-"))
	expectTrailer, expectXref := false, false
	var history [2]any
	var offsets [2]int
	for {
		p.skip()
		start := p.pos
		v, err := p.value(0)
		if err == io.EOF {
			if strict && activeObject {
				return errors.New("unterminated PDF object")
			}
			return nil
		}
		if err != nil {
			return err
		}
		wasValue := expectValue
		if strict && activeObject && !wasValue && !pdfWord(v, "stream") && !pdfWord(v, "endobj") {
			return errors.New("PDF object has trailing syntax")
		}
		if pdfWord(v, "endobj") {
			activeObject = false
		}
		if expectTrailer {
			d, ok := v.(pdfDict)
			if !ok {
				return errors.New("invalid PDF trailer")
			}
			if err := b.xrefPointers(d); err != nil {
				return err
			}
			expectTrailer = false
		}
		if expectXref {
			offset, ok := v.(int64)
			if !ok || offset < 0 {
				return errors.New("invalid PDF startxref")
			}
			b.xrefTargets = append(b.xrefTargets, offset)
			expectXref = false
		}
		if keyword, ok := v.(string); ok && keyword == "startxref" {
			b.lastStartXref = start
			expectXref = true
		}
		if expectValue {
			if err := b.recordInteger(b.current, v); err != nil {
				return err
			}
			expectValue = false
		}
		if keyword, ok := v.(string); ok && keyword == "obj" {
			id, ok1 := history[0].(int64)
			gen, ok2 := history[1].(int64)
			if !ok1 || !ok2 || id < 0 || id > 1<<20 || gen < 0 || gen > 65535 {
				return errors.New("invalid PDF object header")
			}
			activeObject = true
			b.current = pdfRef{id, gen}
			b.currentOffset = int64(offsets[0])
			b.objects[int64(offsets[0])] = b.current
			expectValue = true
		}
		history[0], history[1] = history[1], v
		offsets[0], offsets[1] = offsets[1], start
		if keyword, ok := v.(string); ok && keyword == "xref" {
			b.xrefOffsets[int64(start)] = true
			expectTrailer = true
			if err := p.xref(b); err != nil {
				return err
			}
			previous = nil
			continue
		}
		if keyword, ok := v.(string); !ok || keyword != "stream" {
			previous = v
			continue
		}
		if strict && !activeObject {
			return errors.New("PDF stream is outside an object")
		}
		d, ok := previous.(pdfDict)
		if !ok {
			return errors.New("PDF stream lacks a dictionary")
		}
		n, ok := b.integer(d["Length"])
		if !ok || n < 0 || n > int64(len(data)) {
			return errors.New("PDF stream length cannot be safely resolved")
		}
		if p.pos < len(data) && data[p.pos] == '\r' {
			p.pos++
			if p.pos < len(data) && data[p.pos] == '\n' {
				p.pos++
			}
		} else if p.pos < len(data) && data[p.pos] == '\n' {
			p.pos++
		} else {
			return errors.New("invalid PDF stream newline")
		}
		if n > int64(len(data)-p.pos) {
			return io.ErrUnexpectedEOF
		}
		raw := data[p.pos : p.pos+int(n)]
		p.pos += int(n)
		end, err := p.value(0)
		if err != nil || end != "endstream" {
			return errors.New("PDF stream length does not match endstream")
		}
		if pdfHasName(d["Type"], "XRef") {
			b.xrefOffsets[b.currentOffset] = true
			if err := b.xrefPointers(d); err != nil {
				return err
			}
		}
		if err := b.stream(d, raw, depth); err != nil {
			return err
		}
		previous = nil
	}
}

func (b *pdfStreamBudget) stream(d pdfDict, raw []byte, depth int) error {
	var filters []any
	switch f := d["Filter"].(type) {
	case nil:
	case string:
		if f != "null" {
			return errors.New("invalid PDF filter")
		}
	case pdfName:
		filters = []any{f}
	case []any:
		filters = f
	default:
		return errors.New("indirect PDF filters cannot be safely budgeted")
	}
	if len(filters) > 8 {
		return errors.New("too many PDF stream filters")
	}
	params := []any{d["DecodeParms"]}
	if a, ok := d["DecodeParms"].([]any); ok {
		params = a
	}
	var r io.Reader = bytes.NewReader(raw)
	var closers []io.Closer
	defer func() {
		for _, c := range closers {
			c.Close()
		}
	}()
	for i, f := range filters {
		name, ok := f.(pdfName)
		if !ok {
			return errors.New("invalid PDF filter")
		}
		if i < len(params) && !pdfNull(params[i]) {
			param, ok := params[i].(pdfDict)
			if !ok {
				return errors.New("indirect PDF decode parameters cannot be safely budgeted")
			}
			if pred, exists := param["Predictor"]; exists {
				n, ok := b.integer(pred)
				if !ok || !(n == 1 || n == 2 || n >= 10 && n <= 15) {
					return errors.New("unsupported PDF predictor")
				}
				if n != 1 && (i != len(filters)-1 || pdfHasName(d["Type"], "ObjStm")) {
					return errors.New("PDF predictor combination cannot be safely budgeted")
				}
			}
			// Predictors do not expand beyond their row buffer plus the decoded bytes.
			columns, colors, bits := int64(1), int64(1), int64(8)
			for key, dest := range map[string]*int64{"Columns": &columns, "Colors": &colors, "BitsPerComponent": &bits} {
				if v, exists := param[key]; exists {
					n, ok := b.integer(v)
					if !ok || n <= 0 || n > b.maxPixels {
						return errors.New("PDF predictor exceeds limits")
					}
					*dest = n
				}
			}
			if bits > 16 || colors > 32 || columns > b.maxStream/(colors*bits) {
				return errors.New("PDF predictor exceeds limits")
			}
		}
		switch name {
		case "FlateDecode", "Fl":
			z, err := zlib.NewReader(r)
			if err != nil {
				return fmt.Errorf("PDF Flate stream: %w", err)
			}
			closers = append(closers, z)
			r = z
		case "ASCII85Decode", "A85":
			r = ascii85.NewDecoder(&pdfASCII85Reader{r: r})
		case "ASCIIHexDecode", "AHx":
			r = &pdfHexReader{r: r}
		case "RunLengthDecode", "RL":
			r = &pdfRunLengthReader{r: r}
		case "DCTDecode", "DCT", "CCITTFaxDecode", "CCF":
			w, wok := b.integer(d["Width"])
			h, hok := b.integer(d["Height"])
			if i != len(filters)-1 || !wok || !hok || w <= 0 || h <= 0 || w > b.maxPixels/h {
				return errors.New("PDF encoded image exceeds pixel limit")
			}
			if name == "DCTDecode" || name == "DCT" {
				cfg, err := jpeg.DecodeConfig(r)
				if err != nil {
					return err
				}
				if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width) > b.maxPixels/int64(cfg.Height) {
					return errors.New("PDF JPEG exceeds pixel limit")
				}
			}
			_, err := io.Copy(io.Discard, &pdfBudgetReader{r: r, b: b})
			return err
		default:
			return fmt.Errorf("PDF filter %s cannot be safely budgeted", name)
		}
		// Each stage is bounded before a following decoder can consume it.
		r = &pdfBudgetReader{r: r, b: b}
	}
	limited := &pdfBudgetReader{r: r, b: b}
	if pdfHasName(d["Type"], "ObjStm") || pdfHasName(d["Type"], "XRef") {
		decoded, err := io.ReadAll(limited)
		if err != nil {
			return err
		}
		if pdfHasName(d["Type"], "XRef") {
			return b.xrefStream(d, decoded)
		}
		return b.objectStream(d, decoded)
	}
	_, err := io.Copy(io.Discard, limited)
	return err
}

type pdfBudgetReader struct {
	r io.Reader
	b *pdfStreamBudget
	n int64
}

func (r *pdfBudgetReader) Read(p []byte) (int, error) {
	if time.Now().After(r.b.until) {
		return 0, errors.New("PDF stream decoding timed out")
	}
	room := min(r.b.maxStream-r.n, r.b.maxTotal-r.b.total)
	if room < int64(len(p))-1 {
		p = p[:room+1]
	}
	n, err := r.r.Read(p)
	r.n += int64(n)
	r.b.total += int64(n)
	if r.n > r.b.maxStream || r.b.total > r.b.maxTotal {
		return 0, errors.New("PDF decoded streams exceed size limit")
	}
	return n, err
}

// PDF ASCII85 ends with ~>, unlike encoding/ascii85's raw stream API.
type pdfASCII85Reader struct {
	r    io.Reader
	done bool
}

func (r *pdfASCII85Reader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	var one [1]byte
	for i := range p {
		_, err := io.ReadFull(r.r, one[:])
		if err != nil {
			return i, err
		}
		if one[0] == '~' {
			r.done = true
			return i, io.EOF
		}
		p[i] = one[0]
	}
	return len(p), nil
}

type pdfHexReader struct {
	r    io.Reader
	done bool
}

func (r *pdfHexReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	var one [1]byte
	digits := "0123456789abcdef"
	for i := range p {
		value, count := 0, 0
		for count < 2 {
			_, err := io.ReadFull(r.r, one[:])
			if err != nil {
				return i, err
			}
			c := one[0]
			if c == '>' {
				r.done = true
				if count == 0 {
					return i, io.EOF
				}
				value *= 16
				break
			}
			if pdfSpace(c) {
				continue
			}
			if c >= 'A' && c <= 'F' {
				c += 'a' - 'A'
			}
			v := strings.IndexByte(digits, c)
			if v < 0 {
				return i, errors.New("invalid PDF hex stream")
			}
			value = value*16 + v
			count++
		}
		p[i] = byte(value)
		if r.done {
			return i + 1, io.EOF
		}
	}
	return len(p), nil
}

type pdfRunLengthReader struct {
	r       io.Reader
	pending []byte
	done    bool
}

func (r *pdfRunLengthReader) Read(p []byte) (int, error) {
	if len(r.pending) == 0 {
		if r.done {
			return 0, io.EOF
		}
		var one [1]byte
		if _, err := io.ReadFull(r.r, one[:]); err != nil {
			return 0, err
		}
		n := int(one[0])
		if n == 128 {
			r.done = true
			return 0, io.EOF
		}
		if n < 128 {
			r.pending = make([]byte, n+1)
			if _, err := io.ReadFull(r.r, r.pending); err != nil {
				return 0, err
			}
		} else {
			if _, err := io.ReadFull(r.r, one[:]); err != nil {
				return 0, err
			}
			r.pending = bytes.Repeat(one[:], 257-n)
		}
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func pdfNull(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && s == "null"
}

func pdfDictionaryBudget(d pdfDict) error {
	for _, key := range []string{"Size", "N"} {
		if v, ok := d[key]; ok {
			if n, isInt := v.(int64); isInt && (n < 0 || n > 1<<20) {
				return errors.New("PDF object count exceeds limit")
			}
		}
	}
	if pdfHasName(d["Type"], "XRef") {
		for _, key := range []string{"Size", "W", "Index"} {
			switch v := d[key].(type) {
			case nil:
			case int64:
				if v < 0 || v > 1<<20 {
					return errors.New("PDF xref exceeds limits")
				}
			case []any:
				for _, item := range v {
					n, ok := item.(int64)
					if !ok || n < 0 || n > 1<<20 || (key == "W" && n > 8) {
						return errors.New("PDF xref exceeds limits")
					}
				}
			default:
				return errors.New("indirect PDF xref parameters are unsupported")
			}
		}
	}
	return nil
}

func (p *pdfSyntax) xref(b *pdfStreamBudget) error {
	for {
		v, err := p.value(0)
		if err != nil {
			return err
		}
		if v == "trailer" {
			return nil
		}
		start, ok := v.(int64)
		if !ok || start < 0 || start > 1<<20 {
			return errors.New("invalid PDF xref start")
		}
		v, err = p.value(0)
		if err != nil {
			return err
		}
		count, ok := v.(int64)
		if !ok || count < 0 || count > (1<<20)-start {
			return errors.New("PDF xref exceeds entry limit")
		}
		for i := int64(0); i < count; i++ {
			off, err := p.value(0)
			if err != nil {
				return err
			}
			gen, err := p.value(0)
			if err != nil {
				return err
			}
			state, err := p.value(0)
			if err != nil {
				return err
			}
			offset, ok1 := off.(int64)
			generation, ok2 := gen.(int64)
			if !ok1 || !ok2 || offset < 0 || generation < 0 || generation > 65535 {
				return errors.New("invalid PDF xref entry")
			}
			if state == "n" {
				if err := b.addReference(pdfXrefEntry{ref: pdfRef{start + i, generation}, offset: offset}); err != nil {
					return err
				}
			} else if state != "f" {
				return errors.New("invalid PDF xref entry")
			}
		}
	}
}

func pdfHasName(v any, name string) bool { n, ok := v.(pdfName); return ok && n == pdfName(name) }

func pdfWord(v any, w string) bool { s, ok := v.(string); return ok && s == w }
