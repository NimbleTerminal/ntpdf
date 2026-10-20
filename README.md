# ntpdf — Terminal PDF viewer widget for Bubble Tea

<p>
    <a href="https://github.com/nimbleterminal/ntpdf/tags"><img src="https://img.shields.io/github/tag/nimbleterminal/ntpdf.svg" alt="Latest Release"></a>
    <a href="https://pkg.go.dev/nimbleterminal.dev/ntpdf?tab=doc"><img src="https://godoc.org/github.com/golang/gddo?status.svg" alt="GoDoc"></a>
    <a href="https://github.com/nimbleterminal/ntpdf/blob/main/CODE_OF_CONDUCT.md"><img src="https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg" alt="Code Of Conduct"></a>
</p>

`ntpdf` (`nimbleterminal.dev/ntpdf`) is a [Bubble Tea](https://github.com/charmbracelet/bubbletea) widget that renders PDF documents in the terminal. It pairs [`ledongthuc/pdf`](https://github.com/ledongthuc/pdf) for pure-Go text extraction with [`ntpicture`](https://github.com/nimbleterminal/ntpicture) (`nimbleterminal.dev/ntpicture`) for image rendering — half-block glyphs anywhere, full-resolution Kitty graphics on terminals that support them (Kitty, Ghostty, WezTerm). Browser-based image rendering uses [`@embedpdf/pdfium`](https://www.npmjs.com/package/@embedpdf/pdfium).

[**Try out the live WASM demo.**](https://nimbleterminal.github.io/ntpdf)

<p align="center"><img src="examples/pdfview/demo.gif" alt="ntpdf demo" width="65%"/></p>

The widget has two modes:

- **Text mode** extracts plain text from the PDF and shows styled placeholders for embedded images.
- **Image mode** rasterizes pages via a pluggable `Renderer`. The default backs onto [`go-pdfium`](https://github.com/klippa-app/go-pdfium) running PDFium as WebAssembly (via [wazero](https://wazero.io)) — **CGO-free, no system dependencies, no external binaries.** Bring-your-own renderers (server-rasterized PNGs, MuPDF, etc.) plug in via the same interface.

## Quickstart

```go
package main

import (
    "fmt"
    "os"

    tea "charm.land/bubbletea/v2"
    "nimbleterminal.dev/ntpdf"
)

type model struct{ pv ntpdf.Model }

func (m model) Init() tea.Cmd { return m.pv.Init() }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    if k, ok := msg.(tea.KeyMsg); ok && (k.String() == "q" || k.String() == "ctrl+c") {
        return m, tea.Sequence(m.pv.Dispose(), tea.Quit)
    }
    if sz, ok := msg.(tea.WindowSizeMsg); ok {
        return m, m.pv.SetSize(sz.Width, sz.Height)
    }
    var cmd tea.Cmd
    m.pv, cmd = m.pv.Update(msg)
    return m, cmd
}

func (m model) View() tea.View { return m.pv.View() }

func main() {
    pv := ntpdf.NewWithConfig(ntpdf.Config{InitialPath: os.Args[1]})
    if _, err := tea.NewProgram(model{pv: pv}).Run(); err != nil {
        fmt.Println(err); os.Exit(1)
    }
}
```

Page with `n` / `p`, toggle Text ↔ Image with `m`, swap Glyph ↔ Kitty with `g`, zoom with `+` / `-`, pan with arrows, fit with `f`, reset view with `0`. The widget's `Update` dispatches all of these through `DefaultKeyMap()`; rebind a field to change the keystroke (or set a binding to `key.Binding{}` to disable it).

## Demos

Building requires Go 1.26.8 or newer. See [`examples/README.md`](./examples/README.md) for the full list of controls.

- **`task run-gallery`** ([`examples/gallery`](./examples/gallery/main.go)) — browse embedded multi-page PDF decks, specs, and statements across Image and Text modes, fit policies, and zoom levels.
- **`task run-report`** ([`examples/report`](./examples/report/main.go)) — live multi-page vector PDF report builder with selectable datasets, bar/line charts, and color themes.
- **`task run-inspector`** ([`examples/inspector`](./examples/inspector/main.go)) — split-pane viewer pairing `ImageMode` rasterization with synchronized `TextMode` extraction and live telemetry (`TextView`, `PagePointSize`, `PageDimensions`, `RasterGeometry`).
- **`task run-pagedims`** ([`examples/pagedims`](./examples/pagedims/main.go)) — aspect-matched cell-rectangle demo across `16:9` widescreen, portrait US Letter, and `1:1` square pages.
- **`task run-pdfview`** ([`examples/pdfview`](./examples/pdfview/main.go)) — single-pane CLI viewer (`./bin/ntpdf-pdfview [path.pdf]`) with a status bar, help bubble, and reload key.

```sh
task build-examples
./bin/ntpdf-pdfview path/to/your.pdf
```

All five examples also compile to WebAssembly and run in the browser via [`booba`](https://github.com/NimbleMarkets/go-booba), publishing their key bindings as interactive action buttons above the terminal:

```sh
task serve-wasm-site   # builds web/demos/*.wasm, stages booba + pdfium assets, serves http://localhost:8000/web/
```

The browser demos use booba v0.10.3 and request `ntpicture`'s Kitty shared-memory transport, passing raw RGBA frames without PNG encoding or base64 image payloads, and falling back to direct PNG when the browser bridge is unavailable. Call `Model.Dispose()` (returning the terminal placement cleanup `tea.Cmd`) or `Model.Close()` when disposing the widget.

## Modes

| Mode | What it does | Where it works |
|---|---|---|
| `ntpdf.TextMode` (default) | Plain text via `ledongthuc/pdf`, styled with lipgloss; embedded images shown as boxed placeholders | Everywhere, including WASM |
| `ntpdf.ImageMode` | Rasterized page via a `Renderer` (default: `go-pdfium`/wazero on native, the `@embedpdf/pdfium` JS bridge under `GOOS=js`), fed to `nimbleterminal.dev/ntpicture`. Supports zoom (`+`/`-` up to ×64) and pan (arrow keys when zoomed) | Native + browser-WASM out of the box; WASI needs a custom `RendererFactory` |

`pv.ToggleMode()` returns a `tea.Cmd` that swaps modes and re-renders. Image mode internally uses `ntpicture.PictureGlyph` (universal half-blocks) or `ntpicture.PictureKitty` (high-resolution); switch between them with `pv.ToggleRenderMode()`.

## Renderer

```go
type Renderer interface {
    RenderPage(pageNum, dpi int) (image.Image, error)
    Close() error
}

type RendererFactory func(name string, data []byte) (Renderer, error)
```

The factory is **bytes-first**: the widget reads file contents once (subject to `Limits.MaxFileBytes`) and hands the bytes to the factory, so custom factories never have to implement their own file I/O. Hosts that already have the bytes in memory — an `//go:embed` blob, an HTTP response, a server-pushed buffer — can skip the path round-trip via `pv.SetPDFData(name, data)` or `Config.InitialData` / `InitialName`.

The widget treats `Config.RendererFactory` as optional. When nil, `NewWithConfig` calls `DefaultRendererFactory()`:

- **Native builds** return a `go-pdfium`-backed factory. The first call lazily initialises a wazero pool with the embedded `pdfium.wasm` blob (cold start ~hundreds of ms; warm calls are fast).
- **Browser-WASM (`GOOS=js GOARCH=wasm`)** bridges to [`@embedpdf/pdfium`](https://www.npmjs.com/package/@embedpdf/pdfium) via the [`ntshim/pdfium`](https://github.com/nimbleterminal/ntshim) (`nimbleterminal.dev/ntshim/pdfium`) package. The host loads `web/ntshim/pdfium/pdfium-shim.js` (installed by `go tool ntshim-assets web/ --shim=pdfium`), which fetches the PDFium WebAssembly build from the jsDelivr CDN and populates `window.ntshim.pdfium` — the Go renderer picks it up and uses the same PDFium engine that runs natively. See `web/site.js` for the wiring.
- **WASI (`GOOS=wasip1 GOARCH=wasm`)** returns nil. wazero can technically run interpreted inside WASI but the performance would be unusable for PDF rasterization.

When the factory returns nil (or fails), ImageMode requests fall back to TextMode at View() time. Hosts that ship a server-side rasterizer can wire up their own `RendererFactory` and keep Image mode in WASM. Each loaded document gets its own `Renderer`; `pv.Dispose()` / `pv.Close()` releases the document handle and picture transport resources when a longer-lived host wants explicit cleanup.

### Loading APIs

| Call | When to use |
|---|---|
| `pv.SetPDF(path)` / `Config.InitialPath` | PDF is on disk; widget reads it once (`Limits.MaxFileBytes` enforced via `os.Stat`) |
| `pv.SetPDFData(name, data)` / `Config.InitialData` + `InitialName` | PDF bytes already in memory (embedded, fetched, pre-decompressed); `len(data)` checked against `MaxFileBytes` |
| `pv.SetPageImage(page, img)` | Install a pre-rasterized `image.Image` for a page, bypassing the `Renderer` |

`Config.RenderDPI` (default 300) sets the rasterization resolution; raise it for crisper Kitty output, lower it to trade fidelity for speed.

### Renderer state introspection

- `pv.HasRenderer() bool` — true when the configured factory actually opened a Renderer for the current document. False after a failed factory call (a hostile-doc rasterizer init failure, or no factory configured on WASM). Pair with the next.
- `pv.RendererErr() error` — the most recent factory error, persisted across the document's lifetime (cleared on the next `SetPDF`). Surface it in your status bar to explain why ImageMode is unavailable.

## Resource limits (`Config.Limits`) & Headless Reader (`ntpdf/pdfdoc`)

PDFs are an untrusted-input format; hostile or malformed documents can claim billions of pages, hide objects or cross-reference tables inside strings, chain decompression bombs, demand 100 GB rasterizations, or panic the parser.

Before `ntpdf` hands bytes to `github.com/ledongthuc/pdf` in `TextMode`, it validates and opens the document through [`nimbleterminal.dev/ntpdf/pdfdoc`](./pdfdoc) — a standalone, headless PDF reader package with no Bubble Tea dependency. `pdfdoc.Open(data, limits)` enforces a fail-closed lexical, cross-reference, and stream-inflation preflight before constructing a `Document`, and exposes per-document (`NumPages`, `Version`, `Info`) and per-page (`MediaBox`, `PageSize`, `PageText`, `PageTextRuns`, `PageImages`) queries with synchronous panic recovery.

`Config.Limits` exposes conservative caps applied automatically:

| Field | Default | What it caps |
|---|---|---|
| `MaxFileBytes` | 256 MiB | Input PDF byte length (filesystem `os.Stat` and in-memory byte slice) |
| `MaxPages` | 10 000 | Refuses to load PDFs claiming more pages |
| `MaxStreamBytes` | 256 MiB | Maximum inflated size of any single PDF stream during preflight |
| `MaxInflatedBytes` | 1 GiB | Maximum total inflated size across all PDF streams during preflight |
| `MaxImagesPerPage` | 1024 | Caps image draw enumeration (`Do` XObjects, nested `Form` XObjects, inline `BI`) per page |
| `MaxRenderDPI` | 600 | Per-call DPI clamp applied to every renderer (custom factories included) |
| `MaxRenderPixels` | 100 000 000 | Default renderer queries page size and lowers DPI to fit the bitmap budget (also caps preflight image streams / predictors) |

Set any field to `-1` to disable that cap for trusted input; zero uses the default. Per-page text extraction is panic-guarded — a malformed page yields an empty placeholder rather than crashing the load.

> **Process isolation for untrusted input:** While `ntpdf/pdfdoc` bounds input size, stream inflation, filter chains, predictors, cross-reference offsets, and tree/form recursion, a Go library cannot preempt a hung CPU loop inside an in-process parser once preflight passes. Hosts handling untrusted PDFs that require hard wall-clock termination should run `pdfdoc.Open` and page extraction in a separate, killable worker process (as `gloss` does).

### Sanitizing PDF-derived strings

PDF metadata, error messages, and host-supplied paths can carry C0/C1 control sequences and Unicode bidi format chars ("Trojan Source"-style attacks). The widget's own status text is already sanitized. For host-displayed strings derived from the PDF or its filename — e.g. `RendererErr().Error()` rendered into your status bar — use `ntpdf.SanitizeForTerminal(s string) string`, which drops anything outside Unicode's printable categories and folds newlines / tabs to single spaces.

## Known caveats

- **WASM dependencies.** `wasm.work` selects the Bubble Tea fork with browser support; native builds use the upstream dependency in `go.mod`. The WASM tasks and CI select this workspace automatically.
- **Binary size grows by ~5 MB** from the embedded `pdfium.wasm` blob. The first ImageMode toggle pays a wazero compile cost (hundreds of ms); subsequent renders are fast. Programs that never enter ImageMode skip the cost entirely (pool init is `sync.Once`-gated and deferred to the first factory call).
- **Browser-WASM ImageMode requires the host page to load `web/ntshim/pdfium/pdfium-shim.js`** (installed by `go tool ntshim-assets web/ --shim=pdfium`). The script loads `@embedpdf/pdfium` from the jsDelivr CDN — no npm install or bundler needed. Without it, the renderer factory returns an error on the first ImageMode toggle and ImageMode degrades to TextMode — the rest of the widget keeps working.
- **WASI builds have no built-in image renderer.** `DefaultRendererFactory` returns nil. Hosts that need rasterization there can wire up a server-side rasterizer via `RendererFactory`; custom factories supplied in WASM environments are also responsible for their own resource caps (file size, request timeouts) since `Config.Limits.MaxFileBytes` is only enforced by the default local-filesystem renderer.
- **PDFs are read fully into memory before rasterization.** `go-pdfium`'s `OpenDocument` takes a `[]byte`. For typical documents (single-digit MB) this is fine; multi-hundred-MB PDFs will use proportional RAM. Streaming open is possible via a custom RendererFactory.
- **Per-page text extraction failures (including library panics) drop the page to an empty placeholder** rather than failing the whole load. A page that ledongthuc/pdf can't decode or that triggers a parser panic renders as a blank grid + image placeholder; ImageMode still rasterizes it correctly.
- **Image placeholders count drawn images (`Do` image XObjects, nested `Form` XObjects, and inline `BI` images)** in the page content stream, up to `Limits.MaxImagesPerPage`. Images listed in `/Resources/XObject` that are never drawn on the page are not counted.
- **Kitty placement composition through lipgloss** is best-effort. `ntpicture.Picture.View()` embeds the Kitty placement escapes directly in the rendered string, which composes cleanly inside `lipgloss.JoinVertical` and basic borders. Glyph mode has no such caveat.
- **Vibe coded** This comment was inserted by a human.

## License

The embedded [`Example.pdf`](./examples/pdfview/testdata/Example.pdf) is obtained from [WikiMedia Commons under AGPL](https://commons.wikimedia.org/wiki/File:Example.pdf#Licensing).

Thanks to the [`ledongthuc/pdf`](https://github.com/ledongthuc/pdf) and [`embedpdf/embed-pdf-viewer`](https://github.com/embedpdf/embed-pdf-viewer) PDF libraries and their authors.

[MIT License](./LICENSE.txt) — Copyright (c) 2026 [Neomantra Corp](https://www.neomantra.com).

----
Made with :heart: and :fire: by the team behind [Nimble.Markets](https://nimble.markets).
