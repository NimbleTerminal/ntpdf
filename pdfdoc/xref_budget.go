package pdfdoc

import (
	"errors"
	"fmt"
	"io"
)

// A cross-reference must point at an object the lexical preflight visited.
// Otherwise a reader could follow an offset into a string/comment/stream and
// decode a hidden stream whose bytes the preflight deliberately skipped.
type pdfXrefEntry struct {
	ref        pdfRef
	offset     int64
	compressed bool
	index      int64
}

type pdfXrefTarget struct {
	source string
	offset int64
}

func (b *pdfStreamBudget) checkReferences() error {
	targets := b.xrefTargets
	if b.lastStartXref >= 0 {
		targets = append(targets, b.lastStartXrefTarget)
	}
	for _, target := range targets {
		if !b.xrefOffsets[target.offset] {
			return fmt.Errorf("PDF cross-reference pointer is outside validated boundaries (%s)", target.source)
		}
	}
	for ref := range b.usedIntegers {
		n, ok := b.actualIntegers[ref]
		if !ok || n != b.integers[ref] {
			return fmt.Errorf("PDF indirect integer does not match validated object (object %d)", ref.id)
		}
	}
	for _, x := range b.references {
		if x.compressed {
			ids := b.objectStreams[x.offset]
			if x.index < 0 || x.index >= int64(len(ids)) || ids[x.index] != x.ref.id {
				return fmt.Errorf("PDF xref points outside validated object stream (object %d in stream %d at index %d)", x.ref.id, x.offset, x.index)
			}
		} else if x.offset == 0 {
			if b.usedIntegers[x.ref] {
				return fmt.Errorf("PDF indirect integer resolves to free xref entry at offset 0 (object %d)", x.ref.id)
			}
		} else if ref, ok := b.objects[x.offset]; !ok || ref != x.ref {
			return fmt.Errorf("PDF xref points outside validated object boundaries (object %d at offset %d)", x.ref.id, x.offset)
		}
	}
	return nil
}

func (b *pdfStreamBudget) objectStream(d pdfDict, data []byte) error {
	n, ok1 := d["N"].(int64)
	first, ok2 := d["First"].(int64)
	if !ok1 || !ok2 || n < 0 || n > 65536 || first < 0 || first > int64(len(data)) {
		return errors.New("PDF object stream header exceeds limits")
	}
	p := &pdfSyntax{data: data[:first], until: b.until}
	ids := make([]int64, n)
	offsets := make([]int64, n+1)
	for i := range n {
		id, err := p.value(0)
		if err != nil {
			return err
		}
		offset, err := p.value(0)
		if err != nil {
			return err
		}
		var ok1, ok2 bool
		ids[i], ok1 = id.(int64)
		offsets[i], ok2 = offset.(int64)
		if !ok1 || !ok2 || ids[i] < 0 || ids[i] > 1<<20 || offsets[i] < 0 || offsets[i] > int64(len(data))-first || (i > 0 && offsets[i] <= offsets[i-1]) {
			return errors.New("invalid PDF object stream index")
		}
	}
	offsets[n] = int64(len(data)) - first
	for i := range n {
		q := &pdfSyntax{data: data[first+offsets[i] : first+offsets[i+1]], until: b.until}
		value, err := q.value(0)
		if err != nil {
			return err
		}
		if err := b.recordInteger(pdfRef{ids[i], 0}, value); err != nil {
			return err
		}
		if _, err := q.value(0); err != io.EOF {
			return errors.New("PDF object stream contains trailing syntax")
		}
	}
	if _, exists := b.objectStreams[b.current.id]; exists {
		return errors.New("ambiguous PDF object stream")
	}
	b.objectStreams[b.current.id] = ids
	return nil
}

func (b *pdfStreamBudget) xrefStream(d pdfDict, data []byte) error {
	widths, ok := d["W"].([]any)
	if !ok || len(widths) != 3 {
		return errors.New("invalid PDF xref widths")
	}
	var w [3]int
	row := 0
	for i, v := range widths {
		n, ok := v.(int64)
		if !ok || n < 0 || n > 8 {
			return errors.New("invalid PDF xref width")
		}
		w[i] = int(n)
		row += int(n)
	}
	if row == 0 {
		return errors.New("empty PDF xref row")
	}
	var err error
	data, err = pdfXrefPredictor(d, data, row)
	if err != nil {
		return err
	}
	size, ok := d["Size"].(int64)
	if !ok || size < 0 || size > 1<<20 {
		return errors.New("invalid PDF xref size")
	}
	index := []any{int64(0), size}
	if value, exists := d["Index"]; exists {
		index, ok = value.([]any)
		if !ok {
			return errors.New("invalid PDF xref index")
		}
	}
	if len(index)%2 != 0 {
		return errors.New("invalid PDF xref index")
	}
	for i := 0; i < len(index); i += 2 {
		start, ok1 := index[i].(int64)
		count, ok2 := index[i+1].(int64)
		if !ok1 || !ok2 || start < 0 || count < 0 || start > size || count > size-start || count > int64(len(data)/row) {
			return errors.New("PDF xref index exceeds bounds")
		}
		for j := int64(0); j < count; j++ {
			var fields [3]int64
			for k, n := range w {
				var value uint64
				for _, c := range data[:n] {
					value = value<<8 | uint64(c)
				}
				data = data[n:]
				if value > 1<<63-1 {
					return errors.New("PDF xref integer overflow")
				}
				fields[k] = int64(value)
			}
			if w[0] == 0 {
				fields[0] = 1
			}
			switch fields[0] {
			case 0:
			case 1:
				if err := b.addReference(pdfXrefEntry{ref: pdfRef{start + j, fields[2]}, offset: fields[1]}); err != nil {
					return err
				}
			case 2:
				if err := b.addReference(pdfXrefEntry{ref: pdfRef{start + j, 0}, offset: fields[1], index: fields[2], compressed: true}); err != nil {
					return err
				}
			default:
				return errors.New("invalid PDF xref entry type")
			}
		}
	}
	if len(data) != 0 {
		return errors.New("PDF xref has trailing bytes")
	}
	return nil
}

func pdfXrefPredictor(d pdfDict, data []byte, row int) ([]byte, error) {
	params := d["DecodeParms"]
	if list, ok := params.([]any); ok {
		if len(list) == 0 {
			return data, nil
		}
		params = list[len(list)-1]
	}
	p, ok := params.(pdfDict)
	if !ok {
		return data, nil
	}
	pred, _ := p["Predictor"].(int64)
	if pred == 0 || pred == 1 {
		return data, nil
	}
	columns, _ := p["Columns"].(int64)
	colors, cok := p["Colors"].(int64)
	bits, bok := p["BitsPerComponent"].(int64)
	if pred < 10 || pred > 15 || columns != int64(row) || (cok && colors != 1) || (bok && bits != 8) || len(data)%(row+1) != 0 {
		return nil, errors.New("unsupported PDF xref predictor")
	}
	out := make([]byte, 0, len(data))
	prev := make([]byte, row)
	for len(data) > 0 {
		filter := data[0]
		data = data[1:]
		current := data[:row]
		data = data[row:]
		for i := range current {
			left, above, upperLeft := byte(0), prev[i], byte(0)
			if i > 0 {
				left = current[i-1]
				upperLeft = prev[i-1]
			}
			switch filter {
			case 0:
			case 1:
				current[i] += left
			case 2:
				current[i] += above
			case 3:
				current[i] += byte((int(left) + int(above)) / 2)
			case 4:
				current[i] += pdfPaeth(left, above, upperLeft)
			default:
				return nil, errors.New("invalid PDF xref PNG filter")
			}
		}
		out = append(out, current...)
		copy(prev, current)
	}
	return out, nil
}

func pdfPaeth(a, b, c byte) byte {
	p := int(a) + int(b) - int(c)
	abs := func(n int) int {
		if n < 0 {
			return -n
		}
		return n
	}
	pa, pb, pc := abs(p-int(a)), abs(p-int(b)), abs(p-int(c))
	if pa <= pb && pa <= pc {
		return a
	}
	if pb <= pc {
		return b
	}
	return c
}

func (b *pdfStreamBudget) recordInteger(ref pdfRef, value any) error {
	n, ok := value.(int64)
	if expected, exists := b.integers[ref]; exists && (!ok || n != expected) {
		return errors.New("ambiguous PDF indirect integer")
	}
	if ok {
		b.actualIntegers[ref] = n
	}
	return nil
}

func (b *pdfStreamBudget) addReference(x pdfXrefEntry) error {
	if len(b.references) >= 1<<20 {
		return errors.New("PDF has too many cross references")
	}
	b.references = append(b.references, x)
	return nil
}

func (b *pdfStreamBudget) xrefPointers(d pdfDict) error {
	for _, key := range []string{"Prev", "XRefStm"} {
		if v, exists := d[key]; exists {
			offset, ok := v.(int64)
			if !ok || offset < 0 {
				return fmt.Errorf("invalid PDF cross-reference pointer (/%s)", key)
			}
			b.xrefTargets = append(b.xrefTargets, pdfXrefTarget{
				source: fmt.Sprintf("/%s %d", key, offset),
				offset: offset,
			})
		}
	}
	return nil
}
