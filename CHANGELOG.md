# CHANGELOG

## Unreleased

- **Breaking:** rename module from `github.com/NimbleMarkets/ntcharts-pdf` to `nimbleterminal.dev/ntpdf` (`package ntpdf` at the module root).
- Migrate terminal picture rendering from `github.com/NimbleMarkets/ntcharts/v2/picture` to `nimbleterminal.dev/ntpicture` (`*ntpicture.Picture`).
- Move the browser Bubble Tea `replace` directive out of `go.mod` into `wasm.work` so native consumers use upstream `charm.land/bubbletea/v2` v2.1.0.
- Upgrade `go-booba` to v0.9.2 and `charm.land/bubbletea/v2` to v2.1.0; wire browser demo controls with `bubblehelp` OSC 7501 action publishing and shared-memory RGBA Kitty transport.
- Add `Fit`, `Anchor`, `SetFit`, `SetAnchor`, `PictureOptions`, `Dispose`, `SetTerminal`, `SetResolution`, `RasterGeometry`, and `PictureStatus` on `Model` / `Config`.

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
