# CHANGELOG

## v0.5.0 (2026-10-19)

- Add `nimbleterminal.dev/ntpdf/pdfdoc`, a headless, resource-budgeted PDF reader with fail-closed stream, filter, predictor, object-stream, and cross-reference preflight checks, plus panic-guarded document metadata (`Version`, `Info`, `FormatDate`) and page queries (`MediaBox`, `PageSize`, `PageText`, `PageTextRuns`, `PageImages`).
- Route `ntpdf` text-mode loading (`SetPDF`, `SetPDFData`, `InitialPath`, `InitialData`) through `ntpdf/pdfdoc` and add `MaxStreamBytes` and `MaxInflatedBytes` caps to `ntpdf.Limits`.
- Surface `pdfdoc` metadata (`Version`, `Info`, per-page character count) in the `inspector` demo and emit trailer `/Info` metadata in the example PDF builder.
- Migrate browser-WASM PDFium shim from `github.com/NimbleMarkets/booba-shim` to `nimbleterminal.dev/ntshim` v0.6.0.
- Upgrade `nimbleterminal.dev/ntpicture` to v0.1.1 and `github.com/NimbleMarkets/go-booba` to v0.10.3.

## v0.4.0 (2026-10-17)

- **Breaking:** Rename module from `github.com/NimbleMarkets/ntcharts-pdf` to `nimbleterminal.dev/ntpdf` (`package ntpdf` at the module root).
- Migrate terminal image rendering from `github.com/NimbleMarkets/ntcharts/v2/picture` to `nimbleterminal.dev/ntpicture` v0.1.0.
- Expose `ntpicture.Picture` configuration and lifecycle controls on `Config` and `Model` (`Fit`, `Anchor`, `RenderAuto`, `AutoRenderMode`, `Crop`, `Viewport`, `Stop`, `Keep`, `KeepOnMainScreen`, `Dispose`, `Session`, `TerminalState`, `RasterGeometry`, and `PictureStatus`).
- Add `TextView`, `PagePointSize`, and `PageImageCount` helpers on `Model`.
- Add `gallery`, `report`, and `inspector` demos alongside `pdfview` and `pagedims`, with a multi-demo browser site powered by `go-booba` v0.9.2 and `booba-shim` v0.5.0.
- Move the browser Bubble Tea `replace` directive into `wasm.work` and isolate demo dependencies in `examples/go.mod`.
- Upgrade `go-pdfium` to v1.21.3 and `charm.land/bubbletea/v2` to v2.1.0.

## v0.3.1 (2026-09-29)

 - Update Go dependencies, including NTCharts performance improvements.

## v0.3.0 (2026-06-13)

- **Browser renderer via booba-shim**: The browser-WASM backend now uses
  [`booba-shim/pdfium`](https://github.com/NimbleMarkets/booba-shim)
  instead of a private JS bridge. 

## v0.2.0 (2026-05-22)

### Added
- **Page-Image Cache**: Added a configurable LRU page-image cache for `ImageMode` to optimize page navigation transitions (`PageCacheSize` config option).
- **Dynamic DPI Auto-Scaling**: Added dynamic DPI calculations based on terminal window cell dimensions to improve rendering clarity (`DynamicDPI` config option).

### Changed
- **Resource Finalization**: Upgraded legacy finalization to use Go 1.24's robust `runtime.AddCleanup` for safer background cleanup of JS, native, and WASM Pdfium documents.
- **Test Suite Isolation**: Separated slow WASM integration tests into an isolated test suite (`-tags=integration`) and added a fast local unit test suite (runs in ~0.3s).

### Fixed
- **Terminal Redraws**: Enabled `AltScreen` in full-screen interactive example programs to fix VS Code terminal redraw and scrollback overlap issues.

## v0.1.0 (2026-05-12)

  * Initial release
