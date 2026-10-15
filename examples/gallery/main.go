// Browse embedded multi-page PDF documents across Image and Text modes, fit policies, and zoom levels.
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
	footerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
)

type model struct {
	pv            ntpdf.Model
	samples       []pdfdoc.Sample
	docIdx        int
	width, height int
	preferKitty   bool
}

func newModel() model {
	samples := pdfdoc.GallerySamples(nil)
	cfg := ntpdf.Config{
		DefaultMode: ntpdf.ImageMode,
		InitialData: samples[0].Data,
		InitialName: samples[0].Name,
		DynamicDPI:  runtime.GOOS != "js",
	}
	if runtime.GOOS == "js" {
		cfg.RenderDPI = 120
		cfg.PictureConfig.KittyMedium = picture.KittyMediumSharedMemory
	}
	return model{
		pv:          ntpdf.NewWithConfig(cfg),
		samples:     samples,
		preferKitty: true,
	}
}

func (m model) Init() tea.Cmd { return m.pv.Init() }

func (m model) Help() []key.Binding {
	return []key.Binding{
		demohelp.Key("Next doc", "d", "tab"),
		demohelp.Key("Next page", "n", "]", "pgdown"),
		demohelp.Key("Prev page", "p", "[", "pgup"),
		demohelp.Key("Image / Text", "m", "t"),
		demohelp.Key("Fit mode", "f"),
		demohelp.Key("Zoom in", "+", "="),
		demohelp.Key("Zoom out", "-", "_"),
		demohelp.Key("Glyph / Kitty", "g"),
		demohelp.Key("Reset", "r"),
		demohelp.Key("Quit", "q", "ctrl+c"),
	}
}

func (m model) Summary() string {
	mode := "image"
	if m.pv.Mode() == ntpdf.TextMode {
		mode = "text"
	}
	fit := "contain"
	switch m.pv.Fit() {
	case ntpdf.FitFill:
		fit = "fill"
	case ntpdf.FitCover:
		fit = "cover"
	}
	return fmt.Sprintf("Gallery: doc=%s (%d/%d) page=%d mode=%s fit=%s zoom=%dx",
		filepath.Base(m.pv.Name()), m.docIdx+1, len(m.samples), m.pv.Page(), mode, fit, 1<<m.pv.Zoom())
}

func (m *model) resize() tea.Cmd {
	if m.width <= 0 || m.height <= 0 {
		return nil
	}
	innerW := max(1, m.width-2)
	innerH := max(1, m.height-4)
	return m.pv.SetSize(innerW, innerH)
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
		if key.Matches(msg, m.pv.KeyMap().ToggleRender) {
			m.preferKitty = false
		}
		switch msg.String() {
		case "d", "tab":
			return m, m.selectDoc(m.docIdx + 1)
		case "D", "shift+tab":
			return m, m.selectDoc(m.docIdx - 1)
		case "r":
			fitCmd := m.pv.SetFit(ntpdf.FitContain)
			var modeCmd tea.Cmd
			if m.pv.Mode() != ntpdf.ImageMode {
				modeCmd = m.pv.ToggleMode()
			}
			return m, tea.Batch(fitCmd, modeCmd, m.selectDoc(0))
		}
	}

	var cmd tea.Cmd
	m.pv, cmd = m.pv.Update(msg)
	if m.preferKitty && m.pv.KittySupported() == picture.KittyCapabilitySupported {
		m.preferKitty = false
		if m.pv.RenderMode() != ntpdf.RenderKitty {
			cmd = tea.Batch(cmd, m.pv.ToggleRenderMode())
		}
	}
	return m, cmd
}

func (m model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("loading…")
	}
	header := headerStyle.Render("PDF DOCUMENT GALLERY") + "  " +
		footerStyle.Render("d: doc · n/p: page · m: image/text · f: fit · +/-: zoom · arrows: pan · g: glyph/kitty · q: quit")
	body := boxStyle.Render(m.pv.View().Content)
	statusText := m.Summary()
	if n := m.pv.NumPages(); n > 0 {
		statusText += fmt.Sprintf(" (%d pages)", n)
	}
	status := footerStyle.Width(m.width).Render(statusText)
	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, header, body, status))
	v.AltScreen = true
	return v
}

func main() { demohelp.Main(newModel()) }
