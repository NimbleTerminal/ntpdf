package pdfdoc

import (
	"bytes"
	"compress/zlib"
	"encoding/ascii85"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func budgetStream(filter string, raw []byte) []byte {
	return []byte(fmt.Sprintf("<< /Length %d %s >>\nstream\n%s\nendstream\n", len(raw), filter, raw))
}

func deflateForTest(t *testing.T, data []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zlib.NewWriter(&b)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func ascii85ForTest(data []byte) []byte {
	var b bytes.Buffer
	w := ascii85.NewEncoder(&b)
	_, _ = w.Write(data)
	_ = w.Close()
	b.WriteString("~>")
	return b.Bytes()
}

func smallPDFBudget(data []byte) error {
	b := &pdfStreamBudget{
		maxStream: 1024,
		maxTotal:  4096,
		maxPixels: DefaultMaxImagePixels,
		until:     time.Now().Add(time.Second),
		integers:  map[pdfRef]int64{},
	}
	return b.scan(data, 0)
}

// pdfFile writes objects 1..n and a cross-reference table for them.
func pdfFile(version string, trailer string, objects ...string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "%%PDF-%s\n", version)
	offsets := make([]int, len(objects))
	for i, body := range objects {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R %s >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, trailer, xref)
	return b.Bytes()
}

// samplePDF is a self-contained two-page PDF fixture.
func samplePDF() []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 72 72] /Resources << >> /Contents 5 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 72 72] /Resources << >> /Contents 6 0 R >>",
	}
	for _, c := range []string{"1 0 0", "0 0 1"} {
		stream := c + " rg 0 0 72 72 re f\n"
		objects = append(objects, fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(stream), stream))
	}
	return pdfFile("1.4", "", objects...)
}

// blankPDF is a one-page PDF with nothing drawn on it.
func blankPDF() []byte {
	return pdfFile("1.4", "",
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>",
	)
}

// pagesPDF is a PDF whose pages draw what contents say, with Helvetica as /F1,
// a one-pixel image as /Im1, and a form that draws it twice as /Fm1.
func pagesPDF(contents ...string) []byte {
	var b bytes.Buffer
	var offsets []int
	object := func(body string) {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", len(offsets), body)
	}
	b.WriteString("%PDF-1.4\n")
	kids := make([]string, len(contents))
	for i := range contents {
		kids[i] = fmt.Sprintf("%d 0 R", 6+2*i)
	}
	object("<< /Type /Catalog /Pages 2 0 R >>")
	object(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(contents)))
	object("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	object("<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n\x80\nendstream")
	form := "q /Im1 Do Q q /Im1 Do Q"
	object(fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 1 1] /Resources << /XObject << /Im1 4 0 R >> >> /Length %d >>\nstream\n%s\nendstream", len(form), form))
	for i, c := range contents {
		object(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 3 0 R >> /XObject << /Im1 4 0 R /Fm1 5 0 R >> >> /Contents %d 0 R >>", 7+2*i))
		object(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(c), c))
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, start)
	return b.Bytes()
}

func TestPDFStreamFilterBudgets(t *testing.T) {
	for _, size := range []int{100, 1024, 1025, 8192} {
		raw := bytes.Repeat([]byte(" "), size)
		compressed := deflateForTest(t, raw)
		for _, tc := range []struct {
			name, filter string
			data         []byte
		}{
			{"plain", "", raw},
			{"flate", "/Filter /FlateDecode", compressed},
			{"ascii85-flate", "/Filter [/ASCII85Decode /FlateDecode]", ascii85ForTest(compressed)},
			{"escaped names", "/Filter [/ASCII85Decode /Fl#61teDecode]", ascii85ForTest(compressed)},
		} {
			t.Run(fmt.Sprintf("%s/%d", tc.name, size), func(t *testing.T) {
				err := smallPDFBudget(budgetStream(tc.filter, tc.data))
				if (err != nil) != (size > 1024) {
					t.Fatalf("size %d: %v", size, err)
				}
			})
		}
	}
}

func TestPDFStreamBoundaries(t *testing.T) {
	// Literal stream markers in data and strings cannot truncate the check.
	payload := []byte(strings.Repeat("endstream ", 150))
	if err := smallPDFBudget(budgetStream("", payload)); err == nil {
		t.Fatal("embedded endstream bypassed length")
	}
	if err := smallPDFBudget([]byte("(stream\nignored endstream)\n")); err != nil {
		t.Fatal(err)
	}
	indirect := strings.Replace(string(budgetStream("", []byte("abc"))), "/Length 3", "/Length 9 0 R", 1) + "\n9 0 obj\n3\nendobj\n"
	if err := checkPDFStreams([]byte(indirect), Limits{}); err != nil {
		t.Fatal(err)
	}
	if err := checkPDFStreams([]byte(indirect+"9 0 obj\n400\nendobj\n"), Limits{}); err == nil {
		t.Fatal("ambiguous indirect length accepted")
	}
	for _, bad := range []string{
		"<< /Length 100 >>\nstream\na\nendstream",
		"<< /Length 1 /Filter 9 0 R >>\nstream\na\nendstream",
		"<< /Length 1 /Filter /Unknown >>\nstream\na\nendstream",
		"<< /Length 1 /Filter (FlateDecode) >>\nstream\na\nendstream",
		"<< /Length 1 /Filter <466c6174654465636f6465> >>\nstream\na\nendstream",
		"<< /Length 1 /Length 2 >>\nstream\na\nendstream",
		"xref\n0 999999999\n",
		"<< /Type /XRef /Size 999999999 >>",
	} {
		if err := checkPDFStreams([]byte(bad), Limits{}); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestPDFPredictorAndTotalBudget(t *testing.T) {
	compressed := deflateForTest(t, []byte("small"))
	for _, params := range []string{"/DecodeParms << /Predictor 12 /Columns 2147483647 >>", "/DecodeParms [<< /Columns -1 >>]", "/DecodeParms 9 0 R"} {
		if err := checkPDFStreams(budgetStream("/Filter /FlateDecode "+params, compressed), Limits{}); err == nil {
			t.Fatal("unsafe predictor accepted")
		}
	}
	data := bytes.Repeat(budgetStream("", bytes.Repeat([]byte("x"), 1000)), 5)
	if err := smallPDFBudget(data); err == nil {
		t.Fatal("aggregate decoded budget bypassed")
	}
}

func FuzzPDFStreamPreflight(f *testing.F) {
	f.Add(samplePDF())
	f.Add(budgetStream("", []byte("endstream")))
	f.Add([]byte("<< /DecodeParms [<<>>] /Length 0 /Filter /FlateDecode >>\nstream\n\nendstream"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		_ = checkPDFStreams(data, Limits{})
	})
}

func TestPDFHiddenObjectReferenceRejected(t *testing.T) {
	spoofObj := "4 0 obj\n<< /Length 3 >>\nstream\nabc\nendstream\nendobj"
	for _, tc := range []struct {
		name      string
		container string
	}{
		{"string", "(" + spoofObj + ")"},
		{"comment", "% " + strings.ReplaceAll(spoofObj, "\n", " ") + "\n(ok)"},
		{"skipped stream", fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(spoofObj), spoofObj)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := pdfFile("1.4", "",
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Count 0 /Kids [] >>",
				tc.container,
				"(ordinary)",
			)
			if err := checkPDFStreams(data, Limits{}); err != nil {
				t.Fatalf("baseline checkPDFStreams: %v", err)
			}
			real := bytes.LastIndex(data, []byte("4 0 obj"))
			hidden := bytes.Index(data, []byte("4 0 obj"))
			if hidden == real {
				t.Fatalf("failed to locate distinct hidden 4 0 obj in %s", tc.name)
			}
			mutated := bytes.Replace(data, fmt.Appendf(nil, "%010d 00000 n", real), fmt.Appendf(nil, "%010d 00000 n", hidden), 1)
			err := checkPDFStreams(mutated, Limits{})
			if err == nil {
				t.Fatalf("xref followed an object hidden inside a %s", tc.name)
			}
			wantSub := fmt.Sprintf("object 4 at offset %d", hidden)
			if !strings.Contains(err.Error(), wantSub) {
				t.Fatalf("error %q missing %q", err, wantSub)
			}
		})
	}
}

func TestPDFHiddenCrossReferenceRejected(t *testing.T) {
	spoofXref := "xref\n0 1\n0000000000 65535 f \ntrailer\n<< /Size 1 >>"
	for _, tc := range []struct {
		name      string
		container string
	}{
		{"string", "(" + spoofXref + ")"},
		{"comment", "% " + strings.ReplaceAll(spoofXref, "\n", " ") + "\n(ok)"},
		{"skipped stream", fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(spoofXref), spoofXref)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objects := []string{
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Count 0 /Kids [] >>",
				tc.container,
			}
			base := pdfFile("1.4", "", objects...)
			hidden := bytes.Index(base, []byte("xref"))
			withPrev := pdfFile("1.4", fmt.Sprintf("/Prev %d", hidden), objects...)
			err := checkPDFStreams(withPrev, Limits{})
			if err == nil {
				t.Fatalf("Prev followed a hidden xref inside %s", tc.name)
			}
			wantPrev := fmt.Sprintf("/Prev %d", hidden)
			if !strings.Contains(err.Error(), wantPrev) {
				t.Fatalf("error %q missing %q", err, wantPrev)
			}

			withXRefStm := pdfFile("1.4", fmt.Sprintf("/XRefStm %d", hidden), objects...)
			err = checkPDFStreams(withXRefStm, Limits{})
			if err == nil {
				t.Fatalf("XRefStm followed a hidden xref inside %s", tc.name)
			}
			wantStm := fmt.Sprintf("/XRefStm %d", hidden)
			if !strings.Contains(err.Error(), wantStm) {
				t.Fatalf("error %q missing %q", err, wantStm)
			}
		})
	}

	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Count 0 /Kids [] >>", "(xref\n0 1\n0000000000 65535 f\ntrailer\n<< /Size 1 >>)"}
	data := pdfFile("1.4", "", objects...)
	hidden := bytes.Index(data, []byte("xref\n"))
	data = append(data, []byte(fmt.Sprintf("(\nstartxref\n%d\n)\n%%%%EOF\n", hidden))...)
	if err := checkPDFStreams(data, Limits{}); err == nil {
		t.Fatal("final startxref inside a string was accepted")
	}
}

func TestPDFInUseEntryAtOffsetZero(t *testing.T) {
	// Case 1a: Quartz PDFContext-style unreferenced in-use entry at offset 0
	// (0000000000 00000 n) in a classic xref table must open cleanly.
	base := samplePDF()
	// Append an extra in-use entry at offset 0 (object 183) via a subsection.
	var withUnrefZero bytes.Buffer
	xrefIdx := bytes.Index(base, []byte("trailer\n"))
	withUnrefZero.Write(base[:xrefIdx])
	withUnrefZero.WriteString("183 1\n0000000000 00000 n \n")
	withUnrefZero.Write(base[xrefIdx:])
	doc, err := Open(withUnrefZero.Bytes(), Limits{})
	if err != nil {
		t.Fatalf("unreferenced in-use xref entry at offset 0 rejected: %v", err)
	}
	if doc.NumPages() != 2 {
		t.Fatalf("NumPages() = %d, want 2", doc.NumPages())
	}

	// Case 1b: Referenced in-use entry at offset 0 resolves to null as a free
	// object would in ledongthuc/pdf, so the document still opens cleanly.
	refZero := pdfFile("1.4", "",
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /PieceInfo 4 0 R >>",
		"<< /Unused true >>",
	)
	real4 := bytes.Index(refZero, []byte("4 0 obj"))
	refZero = bytes.Replace(refZero, fmt.Appendf(nil, "%010d 00000 n", real4), []byte("0000000000 00000 n"), 1)
	if _, err := Open(refZero, Limits{}); err != nil {
		t.Fatalf("referenced in-use xref entry at offset 0 rejected: %v", err)
	}

	// Case 1c: An indirect integer used by preflight (e.g. stream /Length 4 0 R)
	// whose xref entry is at offset 0 must be refused naming the object.
	indirectLen := pdfFile("1.4", "",
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 72 72] /Contents 5 0 R >>",
		"3",
		"<< /Length 4 0 R >>\nstream\nq Q\nendstream",
	)
	lenOff := bytes.Index(indirectLen, []byte("4 0 obj"))
	indirectLenZero := bytes.Replace(indirectLen, fmt.Appendf(nil, "%010d 00000 n", lenOff), []byte("0000000000 00000 n"), 1)
	err = checkPDFStreams(indirectLenZero, Limits{})
	if err == nil {
		t.Fatal("indirect /Length resolving to offset-0 xref entry was accepted")
	}
	if !strings.Contains(err.Error(), "object 4") {
		t.Fatalf("error %q did not name object 4", err)
	}
}

func TestPDFLinearizedStartXrefZero(t *testing.T) {
	// Build a small original linearized PDF fixture:
	// First-page section has the /Linearized dictionary, first-page xref, and
	// first-page trailer ending with placeholder `startxref 0`.
	// Main section has the remaining objects, main xref, and final `startxref`
	// pointing at the first-page xref (whose /Prev points at the main xref).
	buildLinearized := func(prevOverride int, finalStartXrefOverride int) ([]byte, int, int) {
		var b bytes.Buffer
		b.WriteString("%PDF-1.4\n")
		off4 := b.Len()
		b.WriteString("4 0 obj\n<< /Linearized 1 /L 1000 /O 6 /E 500 /N 1 /T 800 /H [0 0] >>\nendobj\n")
		off5 := b.Len()
		b.WriteString("5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n")
		off6 := b.Len()
		b.WriteString("6 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 7 0 R >>\nendobj\n")
		off7 := b.Len()
		stream := "BT /F1 12 Tf 10 10 Td (Linearized OK) Tj ET"
		fmt.Fprintf(&b, "7 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", len(stream), stream)

		firstXref := b.Len()
		fmt.Fprintf(&b, "xref\n4 4\n%010d 00000 n \n%010d 00000 n \n%010d 00000 n \n%010d 00000 n \n", off4, off5, off6, off7)

		// Reserve fixed-width /Prev placeholder so offsets after the first-page trailer stay exact.
		prevPlaceholder := "0000000000"
		firstTrailer := fmt.Sprintf("trailer\n<< /Size 8 /Prev %s /Root 1 0 R >>\nstartxref\n0\n%%%%EOF\n", prevPlaceholder)
		firstStartXrefByte := b.Len() + strings.Index(firstTrailer, "startxref")
		b.WriteString(firstTrailer)

		off1 := b.Len()
		b.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
		off2 := b.Len()
		b.WriteString("2 0 obj\n<< /Type /Pages /Kids [6 0 R] /Count 1 >>\nendobj\n")
		off3 := b.Len()
		b.WriteString("3 0 obj\n<< /Info true >>\nendobj\n")

		mainXref := b.Len()
		fmt.Fprintf(&b, "xref\n0 4\n0000000000 65535 f \n%010d 00000 n \n%010d 00000 n \n%010d 00000 n \n", off1, off2, off3)
		finalTarget := firstXref
		if finalStartXrefOverride >= 0 {
			finalTarget = finalStartXrefOverride
		}
		finalTail := fmt.Sprintf("trailer\n<< /Size 8 >>\nstartxref\n%d\n%%%%EOF\n", finalTarget)
		finalStartXrefByte := b.Len() + strings.Index(finalTail, "startxref")
		b.WriteString(finalTail)

		prevTarget := mainXref
		if prevOverride >= 0 {
			prevTarget = prevOverride
		}
		out := bytes.Replace(b.Bytes(), []byte("/Prev "+prevPlaceholder), fmt.Appendf(nil, "/Prev %010d", prevTarget), 1)
		_ = firstStartXrefByte
		return out, prevTarget, finalStartXrefByte
	}

	// Valid linearized PDF opens and extracts text.
	valid, _, _ := buildLinearized(-1, -1)
	doc, err := Open(valid, Limits{})
	if err != nil {
		t.Fatalf("valid linearized PDF rejected: %v", err)
	}
	if doc.NumPages() != 1 {
		t.Fatalf("NumPages() = %d, want 1", doc.NumPages())
	}
	txt, err := doc.PageText(1)
	if err != nil || !strings.Contains(txt, "Linearized OK") {
		t.Fatalf("PageText(1) = %q, %v; want 'Linearized OK'", txt, err)
	}

	// Linearized PDF whose /Prev chain points to a bogus offset is still refused.
	badPrev, prevOff, _ := buildLinearized(5, -1)
	err = checkPDFStreams(badPrev, Limits{})
	if err == nil {
		t.Fatal("linearized PDF with invalid /Prev was accepted")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("/Prev %d", prevOff)) {
		t.Fatalf("error %q missing '/Prev %d'", err, prevOff)
	}

	// A PDF whose final startxref is 0 is still refused with byte offset.
	badFinal, _, finalByte := buildLinearized(-1, 0)
	err = checkPDFStreams(badFinal, Limits{})
	if err == nil {
		t.Fatal("PDF with final startxref 0 was accepted")
	}
	wantFinal := fmt.Sprintf("startxref 0 at byte %d", finalByte)
	if !strings.Contains(err.Error(), wantFinal) {
		t.Fatalf("error %q missing %q", err, wantFinal)
	}
}

func TestPDFCrossReferenceAndObjectStreams(t *testing.T) {
	for _, predictor := range []bool{false, true} {
		t.Run(fmt.Sprint(predictor), func(t *testing.T) {
			var data bytes.Buffer
			data.WriteString("%PDF-1.5\n")
			// Catalog, page tree and page are compressed objects 1..3 inside object 6.
			objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Count 1 /Kids [3 0 R] >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 72 72] /Contents 4 0 R >>"}
			var header, body bytes.Buffer
			for i, obj := range objects {
				fmt.Fprintf(&header, "%d %d ", i+1, body.Len())
				body.WriteString(obj + "\n")
			}
			offsets := map[int]int{}
			offsets[4] = data.Len()
			data.WriteString("4 0 obj\n")
			data.Write(budgetStream("", []byte("q Q\n")))
			data.WriteString("endobj\n")
			offsets[6] = data.Len()
			data.WriteString("6 0 obj\n")
			raw := append(header.Bytes(), body.Bytes()...)
			data.Write(budgetStream(fmt.Sprintf("/Type /ObjStm /N 3 /First %d /Filter /FlateDecode", header.Len()), deflateForTest(t, raw)))
			data.WriteString("endobj\n")
			offsets[5] = data.Len()
			var table bytes.Buffer
			for i := 0; i < 7; i++ {
				var row [7]byte
				switch {
				case i == 0:
					binary.BigEndian.PutUint16(row[5:], 65535)
				case i <= 3:
					row[0] = 2
					binary.BigEndian.PutUint32(row[1:], 6)
					binary.BigEndian.PutUint16(row[5:], uint16(i-1))
				default:
					row[0] = 1
					binary.BigEndian.PutUint32(row[1:], uint32(offsets[i]))
				}
				table.Write(row[:])
			}
			params := ""
			payload := table.Bytes()
			if predictor {
				var predicted bytes.Buffer
				prev := make([]byte, 7)
				for len(payload) > 0 {
					predicted.WriteByte(2)
					for i, c := range payload[:7] {
						predicted.WriteByte(c - prev[i])
						prev[i] = c
					}
					payload = payload[7:]
				}
				payload = predicted.Bytes()
				params = "/DecodeParms << /Predictor 12 /Columns 7 >>"
			}
			data.WriteString("5 0 obj\n")
			data.Write(budgetStream("/Type /XRef /Root 1 0 R /Size 7 /W [1 4 2] /Filter /FlateDecode "+params, deflateForTest(t, payload)))
			data.WriteString("endobj\n")
			fmt.Fprintf(&data, "startxref\n%d\n%%%%EOF\n", offsets[5])
			if err := checkPDFStreams(data.Bytes(), Limits{}); err != nil {
				t.Fatal(err)
			}
			doc, err := Open(data.Bytes(), Limits{})
			if err != nil || doc.NumPages() != 1 {
				t.Fatalf("Open: pages=%v err=%v", doc, err)
			}
			w, h, ok, err := doc.PageSize(1)
			if err != nil || !ok || w != 72 || h != 72 {
				t.Fatalf("PageSize(1) = (%v, %v, %v, %v), want (72, 72, true, nil)", w, h, ok, err)
			}
		})
	}
}

func TestPDFPropertiesAndDates(t *testing.T) {
	data := pdfFile("1.6", "/Info 5 0 R",
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 /MediaBox [0 0 612 792] >>",
		"<< /Type /Page /Parent 2 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595.276 841.89] >>",
		"<< /Title (Field Notes) /Author (R. Naturalist) /Subject (Birds) /Keywords (owls, wrens) /Creator (Quill 2) /Producer (libquill) /CreationDate (D:20260304050607+02'00') /ModDate (D:20260405) >>",
	)
	for date, want := range map[string]string{
		"D:20190429190000Z00'00'": "2019-04-29 19:00:00 UTC",
		"D:20190429190000Z":       "2019-04-29 19:00:00 UTC",
		"D:2019":                  "2019",
		"D:20190429190000-08'00":  "2019-04-29 19:00:00 -08:00",
		"last Tuesday":            "last Tuesday",
		"":                        "",
	} {
		if got := FormatDate(date); got != want {
			t.Errorf("FormatDate(%q) = %q, want %q", date, got, want)
		}
	}

	doc, err := Open(data, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.Version(); got != "1.6" {
		t.Errorf("Version() = %q, want 1.6", got)
	}
	if got := doc.NumPages(); got != 2 {
		t.Errorf("NumPages() = %d, want 2", got)
	}
	info := doc.Info()
	wantInfo := Info{
		Title:        "Field Notes",
		Author:       "R. Naturalist",
		Subject:      "Birds",
		Keywords:     "owls, wrens",
		Creator:      "Quill 2",
		Producer:     "libquill",
		CreationDate: "2026-03-04 05:06:07 +02:00",
		ModDate:      "2026-04-05",
	}
	if info != wantInfo {
		t.Errorf("Info() = %+v, want %+v", info, wantInfo)
	}
	if w, h, ok, err := doc.PageSize(1); err != nil || !ok || w != 612 || h != 792 {
		t.Errorf("PageSize(1) = (%v, %v, %v, %v), want (612, 792, true, nil)", w, h, ok, err)
	}
	if w, h, ok, err := doc.PageSize(2); err != nil || !ok || int(w+0.5) != 595 || int(h+0.5) != 842 {
		t.Errorf("PageSize(2) = (%v, %v, %v, %v), want (~595, ~842, true, nil)", w, h, ok, err)
	}
}

func TestPDFPageTextAndImages(t *testing.T) {
	long := strings.Repeat("words ", 40)
	data := pagesPDF(
		"BT /F1 12 Tf 10 10 Td (Fig. 1) Tj ET q 100 0 0 100 0 0 cm /Im1 Do Q", // A figure and its caption.
		"BT /F1 12 Tf 10 10 Td ("+long+") Tj ET q /Im1 Do Q",                  // Text, with a picture.
		"BT /F1 12 Tf 10 10 Td (Only words.) Tj ET",                           // Text alone (Im1 in Resources is not drawn).
		"q /Fm1 Do Q", // Form XObject drawing Im1 twice, no text.
	)
	doc, err := Open(data, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		page, chars, images int
	}{
		{1, 6, 1},
		{2, len(strings.TrimSpace(long)), 1},
		{3, 11, 0},
		{4, 0, 2},
	} {
		text, err := doc.PageText(c.page)
		if err != nil {
			t.Fatalf("PageText(%d): %v", c.page, err)
		}
		if got := utf8.RuneCountInString(strings.TrimSpace(text)); got != c.chars {
			t.Errorf("page %d chars = %d, want %d (text=%q)", c.page, got, c.chars, text)
		}
		if got := doc.PageImages(c.page); got != c.images {
			t.Errorf("page %d images = %d, want %d", c.page, got, c.images)
		}
	}

	// Verify MaxImageDraws caps image counting on page 4 (which draws 2 images).
	capped, err := Open(data, Limits{MaxImageDraws: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got := capped.PageImages(4); got != 1 {
		t.Errorf("capped PageImages(4) = %d, want 1", got)
	}

	// Verify blankPDF opens and yields empty text and 0 images.
	blank, err := Open(blankPDF(), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if txt, err := blank.PageText(1); err != nil || strings.TrimSpace(txt) != "" {
		t.Errorf("blank PageText(1) = %q, %v", txt, err)
	}
	if got := blank.PageImages(1); got != 0 {
		t.Errorf("blank PageImages(1) = %d, want 0", got)
	}
}

func TestOpenEnforcesLimits(t *testing.T) {
	valid := samplePDF()
	if _, err := Open(nil, Limits{}); err == nil {
		t.Fatal("expected error on empty data")
	}
	if _, err := Open(valid, Limits{MaxFileBytes: 10}); err == nil {
		t.Fatal("expected error when exceeding MaxFileBytes")
	}
	if _, err := Open(valid, Limits{MaxPages: 1}); err == nil || !strings.Contains(err.Error(), "MaxPages") {
		t.Fatalf("expected MaxPages error, got %v", err)
	}
	if _, err := Open(valid, Limits{Deadline: time.Now().Add(-time.Second)}); err == nil {
		t.Fatal("expected error on expired Deadline")
	}
}

