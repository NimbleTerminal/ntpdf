package pdfdoc

import (
	"bytes"
	"encoding/ascii85"
	"encoding/hex"
	"io"
	"testing"
)

// Valid encodings reach decoder states that whole-PDF mutation seldom visits.
// Include literal and repeat runs, PDF whitespace, and the end markers.
func FuzzPDFStreamFilterRoundTrip(f *testing.F) {
	for _, seed := range [][]byte{nil, {0}, []byte("hello"), bytes.Repeat([]byte{255}, 129)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 8192 {
			t.Skip()
		}
		hexData := []byte(hex.EncodeToString(raw) + "\n>")
		literal := make([]byte, 0, len(raw)+len(raw)/128+2)
		for rest := raw; len(rest) > 0; {
			n := min(128, len(rest))
			literal = append(literal, byte(n-1))
			literal = append(literal, rest[:n]...)
			rest = rest[n:]
		}
		literal = append(literal, 128)
		repeat := make([]byte, 0, 2*len(raw)+1)
		for _, b := range raw {
			repeat = append(repeat, 255, b) // two identical bytes per run
		}
		repeat = append(repeat, 128)
		doubled := make([]byte, 0, 2*len(raw))
		for _, b := range raw {
			doubled = append(doubled, b, b)
		}
		for _, tc := range []struct {
			name string
			r    io.Reader
			want []byte
		}{
			{"hex", &pdfHexReader{r: bytes.NewReader(hexData)}, raw},
			{"ascii85", ascii85.NewDecoder(&pdfASCII85Reader{r: bytes.NewReader(ascii85ForTest(raw))}), raw},
			{"literal", &pdfRunLengthReader{r: bytes.NewReader(literal)}, raw},
			{"repeat", &pdfRunLengthReader{r: bytes.NewReader(repeat)}, doubled},
		} {
			got, err := io.ReadAll(tc.r)
			if err != nil || !bytes.Equal(got, tc.want) {
				t.Fatalf("%s round trip: got %x, want %x, error %v", tc.name, got, tc.want, err)
			}
		}
	})
}

// Malformed streams must produce the same bytes and terminal error regardless
// of the consumer's buffer size. Output is bounded by RunLength's expansion.
func FuzzPDFStreamFilterChunks(f *testing.F) {
	for _, seed := range [][]byte{[]byte("a >"), []byte("00\nFF>ignored"), {0, 'a', 128}, {129, 'x', 128}, {3, 'a'}, {255}} {
		f.Add(seed, uint8(0))
		f.Add(seed, uint8(1))
	}
	f.Fuzz(func(t *testing.T, data []byte, kind uint8) {
		if len(data) > 4096 {
			t.Skip()
		}
		read := func(size int) ([]byte, string) {
			var r io.Reader = &pdfHexReader{r: bytes.NewReader(data)}
			if kind%2 != 0 {
				r = &pdfRunLengthReader{r: bytes.NewReader(data)}
			}
			buf := make([]byte, size)
			var out []byte
			for {
				n, err := r.Read(buf)
				if n < 0 || n > len(buf) || (n == 0 && err == nil) {
					t.Fatal("decoder violated the Reader contract or made no progress")
				}
				out = append(out, buf[:n]...)
				if len(out) > 128*len(data) {
					t.Fatal("decoder exceeded maximum expansion")
				}
				if err != nil {
					return out, err.Error()
				}
			}
		}
		want, wantErr := read(1)
		for _, size := range []int{7, 512} {
			got, err := read(size)
			if !bytes.Equal(got, want) || err != wantErr {
				t.Fatalf("chunk size %d changes decoded output or error: %q vs %q", size, err, wantErr)
			}
		}
	})
}
