# Examples

Run these from the repository root. Each browser demo publishes its keys through
booba so the same actions appear as interactive buttons above the terminal.

| Command | What it shows | Controls |
| --- | --- | --- |
| `task run-gallery` | Multi-document PDF gallery (`01-architecture-deck.pdf` .. `04-aspect-calibration.pdf`) across Image & Text modes | `d`: next doc, `n`/`p`: next/prev page, `m`: image/text, `f`: fit mode, `+/-`: zoom, arrows: pan, `g`: glyph/kitty, `r`: reset |
| `task run-report` | Live multi-page vector PDF report builder with selectable datasets, chart styles, and themes | `t`: bar/line, `d`: dataset, `c`: theme, `v`: vary values, `n`/`p`: page, `m`: image/text, `f`: fit mode, `g`: glyph/kitty, `r`: reset |
| `task run-inspector` | Side-by-side split pane pairing `ImageMode` rasterization with synchronized `TextMode` extraction and live telemetry | `d`: next doc, `n`/`p`: next/prev page, `f`: fit mode, `+/-`: zoom, arrows: pan, `g`: glyph/kitty, `r`: reset |
| `task run-pagedims` | Aspect-matched cell rectangle sizing across `16:9` widescreen, portrait US Letter, and `1:1` square pages | `n`/`p`: next/prev page, `a`: aspect lock, `m`: image/text, `g`: glyph/kitty, `r`: reset |
| `task run-pdfview` | Single-pane CLI viewer (`bin/ntpdf-pdfview [path.pdf]`) with zoom, pan, fit modes, and Image/Text toggle | `n`/`p`: next/prev page, `m`: image/text, `f`: fit mode, `+/-`: zoom, arrows: pan, `g`: glyph/kitty, `r`: reset |

Use `q` or `Ctrl+C` to quit. `task serve-wasm-site` builds and serves all five
interactive demos at `http://localhost:8000/web/`.
