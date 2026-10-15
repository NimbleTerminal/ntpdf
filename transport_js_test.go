//go:build js && wasm

package ntpdf

import (
	"image"
	"syscall/js"
	"testing"

	picture "nimbleterminal.dev/ntpicture"
)

func TestBrowserSharedMemoryLifecycle(t *testing.T) {
	old := js.Global().Get("ghosttyKittySharedMemory")
	registry := js.Global().Get("Map").New()
	js.Global().Set("ghosttyKittySharedMemory", registry)
	t.Cleanup(func() { js.Global().Set("ghosttyKittySharedMemory", old) })
	picture.ForceKittyCapability(picture.KittyCapabilitySupported)
	t.Cleanup(func() { picture.ForceKittyCapability(picture.KittyCapabilityUnknown) })

	m := NewWithConfig(Config{Cols: 2, Rows: 2, DefaultMode: ImageMode, PictureConfig: picture.Config{
		KittyMedium: picture.KittyMediumSharedMemory,
	}})
	defer m.Close()
	m = driveCmd(m, m.ToggleRenderMode())
	m = driveCmd(m, m.SetPageImage(1, image.NewNRGBA(image.Rect(0, 0, 2, 2))))
	if st := m.PictureStatus(); st.Medium != picture.KittyMediumSharedMemory || st.Format != picture.KittyFormatRGBA {
		t.Fatalf("expected shared-memory RGBA, got medium=%v format=%v", st.Medium, st.Format)
	}
	if registry.Get("size").Int() != 1 {
		t.Fatal("expected one pending shared-memory frame")
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if registry.Get("size").Int() != 0 {
		t.Fatal("Close leaked shared-memory frame without a PDF renderer")
	}

	// Older browser terminals must still render through direct PNG.
	js.Global().Set("ghosttyKittySharedMemory", js.Undefined())
	m = driveCmd(m, m.ToggleRenderMode())
	m = driveCmd(m, m.SetPageImage(1, image.NewNRGBA(image.Rect(0, 0, 2, 2))))
	if st := m.PictureStatus(); st.Medium != picture.KittyMediumDirect || st.Format != picture.KittyFormatPNG {
		t.Fatalf("expected direct PNG fallback, got medium=%v format=%v", st.Medium, st.Format)
	}
}
