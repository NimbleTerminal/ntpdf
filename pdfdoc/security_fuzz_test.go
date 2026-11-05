package pdfdoc

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"strings"
	"testing"
	"time"
)

func fuzzPreflightLimits() Limits {
	return Limits{MaxFileBytes: 1 << 20, MaxStreamBytes: 64 << 10, MaxInflatedBytes: 256 << 10, MaxImagePixels: 4096, Timeout: time.Second}
}

func fuzzDeflate(t *testing.T, raw []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	w := zlib.NewWriter(&out)
	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// Preserve outer document offsets, stream lengths, and compression while
// mutating inner structures. Random whole-file mutation rarely gets this far.
func FuzzPDFStructuredPreflight(f *testing.F) {
	f.Add(uint8(0), []byte("0000000009"), int64(0))
	f.Add(uint8(1), []byte("<< /Example true >>"), int64(0))
	f.Add(uint8(2), []byte{0, 0, 0, 0, 0, 255, 255}, int64(1))
	f.Add(uint8(3), []byte("q Q\n"), int64(1))
	f.Add(uint8(3), []byte{0, 1, 2, 3}, int64(12))
	f.Fuzz(func(t *testing.T, kind uint8, payload []byte, parameter int64) {
		if len(payload) > 8192 {
			t.Skip()
		}
		var data []byte
		switch kind % 4 {
		case 0:
			data = blankPDF()
			// Change a live xref entry without damaging its surrounding syntax or
			// shifting startxref. Successful parsing must still validate the offset.
			real := bytes.Index(data, []byte("3 0 obj"))
			entry := fmt.Appendf(nil, "%010d 00000 n", real)
			replacement := fmt.Appendf(nil, "%010d 00000 n", parameter)
			data = bytes.Replace(data, entry, replacement, 1)
		case 1:
			header := fmt.Appendf(nil, "7 %d ", parameter)
			raw := append(header, payload...)
			objectStream := budgetStream(fmt.Sprintf("/Type /ObjStm /N 1 /First %d /Filter /FlateDecode", len(header)), fuzzDeflate(t, raw))
			data = pdfFile("1.5", "", "<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Count 0 /Kids [] >>", string(objectStream))
		case 2:
			// Seed a valid free xref-stream entry, then mutate its binary records,
			// width specification, and declared entry count behind valid compression.
			xref := budgetStream(fmt.Sprintf("/Type /XRef /Size %d /W [1 4 2] /Filter /FlateDecode", parameter), fuzzDeflate(t, payload))
			data = pdfFile("1.5", "", "<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Count 0 /Kids [] >>", string(xref))
		case 3:
			chain := ascii85ForTest(fuzzDeflate(t, payload))
			stream := budgetStream(fmt.Sprintf("/Filter [/ASCII85Decode /FlateDecode] /DecodeParms [null << /Predictor %d /Columns 4 /Colors 1 /BitsPerComponent 8 >>]", parameter), chain)
			data = pdfFile("1.4", "", "<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Count 0 /Kids [] >>", string(stream))
		}
		// Do not use checkPDFStreams: its public panic recovery would hide a
		// parser failure from Go's fuzz crash minimizer.
		_ = checkPDFStreamSyntax(data, fuzzPreflightLimits())
	})
}

// A valid document must become invalid when a live xref entry is redirected
// into a string, comment, or length-delimited stream, regardless of its bytes.
func FuzzPDFXrefBoundaries(f *testing.F) {
	for kind := uint8(0); kind < 3; kind++ {
		f.Add(kind, []byte("abc"))
	}
	f.Fuzz(func(t *testing.T, kind uint8, payload []byte) {
		if len(payload) > 4096 {
			t.Skip()
		}
		hidden := fmt.Sprintf("4 0 obj\n<< /Length %d >>\nstream\n%sendstream\nendobj", len(payload), payload)
		var container string
		switch kind % 3 {
		case 0:
			escaped := strings.NewReplacer("\\", "\\\\", "(", "\\(", ")", "\\)").Replace(hidden)
			container = "(" + escaped + ")"
		case 1:
			container = "% " + strings.NewReplacer("\r", " ", "\n", " ").Replace(hidden) + "\n(ordinary)"
		case 2:
			container = string(budgetStream("", []byte(hidden)))
		}
		data := pdfFile("1.4", "", "<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Count 0 /Kids [] >>", container, "(actual fourth object)")
		if err := checkPDFStreamSyntax(data, fuzzPreflightLimits()); err != nil {
			// The strict preflight may reject otherwise legal payloads (for example,
			// conflicting integer declarations hidden in strings). Only mutate
			// documents it accepts, so rejection is the expected change.
			return
		}
		real := bytes.LastIndex(data, []byte("4 0 obj"))
		hiddenOffset := bytes.Index(data, []byte("4 0 obj"))
		if real == hiddenOffset {
			t.Fatal("fixture lacks hidden object")
		}
		data = bytes.Replace(data, fmt.Appendf(nil, "%010d 00000 n", real), fmt.Appendf(nil, "%010d 00000 n", hiddenOffset), 1)
		if err := checkPDFStreamSyntax(data, fuzzPreflightLimits()); err == nil {
			t.Fatal("xref accepted object inside unvalidated boundary")
		}
	})
}
