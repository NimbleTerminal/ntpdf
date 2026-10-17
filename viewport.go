// viewport.go: zoom + pan plumbing for ImageMode.
//
// The viewport is described in normalized [0, 1] source-image coords:
// (panX, panY) is the top-left corner of the visible rectangle and
// (1/2^zoom) is its width and height in normalized units. Storing pan
// as a fraction (rather than pixels) lets the viewport survive a
// re-render at a different DPI or cell-rect size.

package ntpdf

import (
	"image"
	"image/draw"
	"math"

	tea "charm.land/bubbletea/v2"
)

const (
	// zoomMax bounds the magnification. 6 means a 64× zoom — fine enough
	// to see individual rasterized pixels at 300 DPI on a Letter-sized
	// page (the cropped source becomes ~40×52 px, scaled back up to the
	// cell rect with maximum chunkiness).
	zoomMax = 6

	// panStep is the fraction of a viewport that one PanLeft/Right/Up/Down
	// press travels. 0.25 means four presses traverse the full visible
	// area — fine for inspection without feeling sluggish.
	panStep = 0.25
)

// subImager is the SubImage signature implemented by concrete image
// types in image/. *image.RGBA (pdfium's output, plus our deep-copy),
// *image.NRGBA, *image.YCbCr etc. all satisfy it. Hosts that hand us
// other image types via SetPageImage fall through to the draw.Draw path
// in cropToViewport.
type subImager interface {
	SubImage(r image.Rectangle) image.Image
}

// viewportSize returns the normalized [0, 1] side length of the current
// visible rectangle in source-image coords.
func (m Model) viewportSize() float64 {
	z := m.zoom
	if z < 0 {
		z = 0
	}
	if z > 30 {
		z = 30
	}
	return 1.0 / float64(uint(1)<<uint(z))
}

// viewportCenter returns the current viewport's center in normalized
// [0, 1] source-image coords.
func (m Model) viewportCenter() (cx, cy float64) {
	vp := m.viewportSize()
	return m.panX + vp/2, m.panY + vp/2
}

// recenterTo positions the viewport so its center sits at (cx, cy),
// clamping so the viewport stays inside [0, 1] on both axes. Called by
// ZoomIn / ZoomOut to keep what the user is looking at fixed across
// zoom transitions.
func (m *Model) recenterTo(cx, cy float64) {
	vp := m.viewportSize()
	m.panX = cx - vp/2
	m.panY = cy - vp/2
	m.clampPan()
}

// clampPan keeps the viewport rectangle inside [0, 1] × [0, 1]. Called
// after every mutation of zoom or pan.
func (m *Model) clampPan() {
	if math.IsNaN(m.panX) || math.IsInf(m.panX, 0) {
		m.panX = 0
	}
	if math.IsNaN(m.panY) || math.IsInf(m.panY, 0) {
		m.panY = 0
	}
	vp := m.viewportSize()
	if m.panX < 0 {
		m.panX = 0
	}
	if m.panY < 0 {
		m.panY = 0
	}
	if m.panX+vp > 1 {
		m.panX = 1 - vp
	}
	if m.panY+vp > 1 {
		m.panY = 1 - vp
	}
	// At zoom 0 the viewport is the whole image; force pan to origin.
	if m.zoom == 0 {
		m.panX, m.panY = 0, 0
	}
}

// pan applies a (dx, dy) step where each unit is one panStep in
// viewport-size units. Direction sign follows screen convention:
// dx > 0 pans right, dy > 0 pans down.
func (m *Model) pan(dx, dy float64) tea.Cmd {
	if m.mode != ImageMode || m.sourceImage == nil || m.zoom == 0 {
		return nil
	}
	step := panStep * m.viewportSize()
	m.panX += dx * step
	m.panY += dy * step
	m.clampPan()
	return m.applyViewport()
}

// applyViewport configures the source-pixel crop rectangle on the
// embedded picture.Picture and submits the source image when needed.
func (m *Model) applyViewport() tea.Cmd {
	m.ensurePicture()
	if m.sourceImage == nil {
		m.sourceImagePresented = false
		return m.presentImageCropped(nil, image.Rectangle{}, true)
	}
	crop := viewportCropRect(m.sourceImage, m.zoom, m.panX, m.panY)
	if !m.sourceImagePresented {
		m.sourceImagePresented = true
		return m.presentImageCropped(m.sourceImage, crop, m.sourceImmutable)
	}
	return m.pic.SetCrop(crop)
}

// viewportCropRect returns the source-pixel rectangle described by
// (zoom, panX, panY) in normalized source-image coords. At zoom 0 it
// returns an empty rectangle (full source image).
func viewportCropRect(src image.Image, zoom int, panX, panY float64) image.Rectangle {
	if src == nil || zoom <= 0 {
		return image.Rectangle{}
	}
	if zoom > 30 {
		zoom = 30
	}
	if math.IsNaN(panX) || math.IsInf(panX, 0) {
		panX = 0
	}
	if math.IsNaN(panY) || math.IsInf(panY, 0) {
		panY = 0
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	vp := 1.0 / float64(uint(1)<<uint(zoom))
	x0 := b.Min.X + int(panX*float64(w)+0.5)
	y0 := b.Min.Y + int(panY*float64(h)+0.5)
	rectW := int(vp*float64(w) + 0.5)
	rectH := int(vp*float64(h) + 0.5)
	if rectW < 1 {
		rectW = 1
	}
	if rectH < 1 {
		rectH = 1
	}
	if x0+rectW > b.Max.X {
		x0 = b.Max.X - rectW
	}
	if y0+rectH > b.Max.Y {
		y0 = b.Max.Y - rectH
	}
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	return image.Rect(x0, y0, x0+rectW, y0+rectH)
}

// cropToViewport returns the sub-image described by (zoom, panX, panY)
// in normalized source-image coords. At zoom 0 it returns src unchanged.
func cropToViewport(src image.Image, zoom int, panX, panY float64) image.Image {
	cropRect := viewportCropRect(src, zoom, panX, panY)
	if cropRect.Empty() {
		return src
	}
	if si, ok := src.(subImager); ok {
		return si.SubImage(cropRect)
	}
	out := image.NewRGBA(image.Rect(0, 0, cropRect.Dx(), cropRect.Dy()))
	draw.Draw(out, out.Bounds(), src, cropRect.Min, draw.Src)
	return out
}
