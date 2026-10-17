// Side-by-side PDF inspector pairing ImageMode rasterization with synchronized TextMode extraction and telemetry.
package main

import (
	"fmt"
	"path/filepath"
	"runtime"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	picture "nimbleterminal.dev/ntpicture"

	"nimbleterminal.dev/ntpdf"
	"nimbleterminal.dev/ntpdf/examples/internal/demohelp"
	"nimbleterminal.dev/ntpdf/examples/internal/pdfdoc"
)

var (
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#8de3b6"))
	labelStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#4ea8de"))
	footerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
)

type model struct {
	pv            ntpdf.Model
	samples       []pdfdoc.Sample
	docIdx        int
	width, height int
}

func newModel() model {
	samples := pdfdoc.GallerySamples(nil)
	cfg := ntpdf.Config{
		DefaultMode: ntpdf.ImageMode,
		InitialData: samples[0].Data,
		InitialName: samples[0].Name,
		DynamicDPI:  runtime.GOOS != "js",
	}
	cfg.PictureOptions.AutoMode = true
	if runtime.GOOS == "js" {
		cfg.RenderDPI = 120
		cfg.PictureConfig.KittyMedium = picture.KittyMediumSharedMemory
	}
	return model{
		pv:      ntpdf.NewWithConfig(cfg),
		samples: samples,
	}
}

func (m model) Init() tea.Cmd { return m.pv.Init() }

func (m model) Help() []key.Binding {
	return []key.Binding{
		demohelp.Key("Next doc", "d", "tab"),
		demohelp.Key("Next page", "n", "]", "pgdown"),
		demohelp.Key("Prev page", "p", "[", "pgup"),
		demohelp.Key("Fit mode", "f"),
		demohelp.Key("Zoom in", "+", "="),
		demohelp.Key("Zoom out", "-", "_"),
		demohelp.Key("Glyph / Kitty", "g"),
		demohelp.Key("Reset", "r"),
		demohelp.Key("Quit", "q", "ctrl+c"),
	}
}

func (m model) Summary() string {
	fit := "contain"
	switch m.pv.Fit() {
	case ntpdf.FitFill:
		fit = "fill"
	case ntpdf.FitCover:
		fit = "cover"
	}
	return fmt.Sprintf("Inspector: doc=%s (%d/%d) page=%d fit=%s zoom=%dx",
		filepath.Base(m.pv.Name()), m.docIdx+1, len(m.samples), m.pv.Page(), fit, 1<<m.pv.Zoom())
}

func (m model) paneDims() (leftW, rightW, innerH int) {
	totalInnerW := max(20, m.width-4)
	leftW = max(10, totalInnerW/2)
	rightW = max(10, totalInnerW-leftW)
	innerH = max(4, m.height-4)
	return leftW, rightW, innerH
}

func (m *model) resize() tea.Cmd {
	if m.width <= 0 || m.height <= 0 {
		return nil
	}
	leftW, _, innerH := m.paneDims()
	return m.pv.SetSize(leftW, innerH)
}

func (m *model) selectDoc(idx int) tea.Cmd {
	n := len(m.samples)
	m.docIdx = ((idx % n) + n) % n
	s := m.samples[m.docIdx]
	return m.pv.SetPDFData(s.Name, s.Data)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, m.resize()

	case tea.KeyMsg:
		if demohelp.Quit(msg) {
			return m, tea.Sequence(m.pv.Dispose(), tea.Quit)
		}
		switch msg.String() {
		case "d", "tab":
			return m, m.selectDoc(m.docIdx + 1)
		case "D", "shift+tab":
			return m, m.selectDoc(m.docIdx - 1)
		case "m", "t":
			// Keep the left pane in ImageMode since the right pane always shows TextView.
			return m, nil
		case "r":
			fitCmd := m.pv.SetFit(ntpdf.FitContain)
			resetCmd := m.pv.ResetView()
			return m, tea.Batch(fitCmd, resetCmd, m.selectDoc(0))
		}
	}

	var cmd tea.Cmd
	m.pv, cmd = m.pv.Update(msg)
	return m, cmd
}

func (m model) telemetryLine(width int) string {
	page := m.pv.Page()
	wPts, hPts, _ := m.pv.PagePointSize(page)
	pxW, pxH, hasPx := m.pv.PageDimensions(page)
	rg := m.pv.RasterGeometry()
	proto := "glyph"
	if m.pv.RenderMode() == ntpdf.RenderKitty {
		proto = "kitty"
	}
	pxInfo := "pending"
	if hasPx {
		pxInfo = fmt.Sprintf("%dx%dpx", pxW, pxH)
	}
	info := fmt.Sprintf("Media:%.0fx%.0fpt  Bitmap:%s  Target:%dx%dpx (%dx%dc)  Proto:%s  Images:%d",
		wPts, hPts, pxInfo, rg.Pixels.X, rg.Pixels.Y, rg.Cells.X, rg.Cells.Y, proto, m.pv.PageImageCount(page))
	return labelStyle.Width(width).MaxWidth(width).Render(info)
}

func (m model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("loading…")
	}
	_, rightW, innerH := m.paneDims()
	header := headerStyle.Render("SIDE-BY-SIDE PDF INSPECTOR") + "  " +
		footerStyle.Render("Left: ImageMode · Right: TextMode + Telemetry · d: doc · n/p: page · f: fit · +/-: zoom · q: quit")

	leftPane := boxStyle.Render(m.pv.View().Content)

	textH := max(1, innerH-1)
	textContent := m.pv.TextView(rightW, textH)
	telemetry := m.telemetryLine(rightW)
	rightPane := boxStyle.Render(lipgloss.JoinVertical(lipgloss.Left, textContent, telemetry))

	panes := lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)
	status := footerStyle.Width(m.width).Render(m.Summary())
	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, header, panes, status))
	v.AltScreen = true
	return v
}

func main() { demohelp.Main(newModel()) }
