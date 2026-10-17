// Live multi-page vector PDF report builder rendered in ntpdf.Model.
package main

import (
	"fmt"
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
	lineMode      bool
	dataIdx       int
	themeIdx      int
	variant       int
	width, height int
}

func newModel() model {
	name, data := pdfdoc.BuildReport(false, 0, 0, 0)
	cfg := ntpdf.Config{
		DefaultMode: ntpdf.ImageMode,
		InitialData: data,
		InitialName: name,
		DynamicDPI:  runtime.GOOS != "js",
	}
	cfg.PictureOptions.AutoMode = true
	if runtime.GOOS == "js" {
		cfg.RenderDPI = 120
		cfg.PictureConfig.KittyMedium = picture.KittyMediumSharedMemory
	}
	return model{
		pv: ntpdf.NewWithConfig(cfg),
	}
}

func (m *model) rebuildReport() tea.Cmd {
	name, data := pdfdoc.BuildReport(m.lineMode, m.dataIdx, m.themeIdx, m.variant)
	return m.pv.SetPDFData(name, data)
}

func (m model) Init() tea.Cmd { return m.pv.Init() }

func (m model) Help() []key.Binding {
	return []key.Binding{
		demohelp.Key("Bar / Line", "t"),
		demohelp.Key("Next dataset", "d", "tab"),
		demohelp.Key("Theme", "c"),
		demohelp.Key("Vary values", "v"),
		demohelp.Key("Next page", "n", "]", "pgdown"),
		demohelp.Key("Prev page", "p", "[", "pgup"),
		demohelp.Key("Image / Text", "m"),
		demohelp.Key("Fit mode", "f"),
		demohelp.Key("Glyph / Kitty", "g"),
		demohelp.Key("Reset", "r"),
		demohelp.Key("Quit", "q", "ctrl+c"),
	}
}

func (m model) Summary() string {
	kind := "bar"
	if m.lineMode {
		kind = "line"
	}
	mode := "image"
	if m.pv.Mode() == ntpdf.TextMode {
		mode = "text"
	}
	return fmt.Sprintf("Report: type=%s dataset=%s theme=%s variant=%d page=%d mode=%s",
		kind,
		pdfdoc.ReportDatasets[m.dataIdx].Key,
		pdfdoc.Themes[m.themeIdx].Name,
		m.variant,
		m.pv.Page(),
		mode,
	)
}

func (m *model) resize() tea.Cmd {
	if m.width <= 0 || m.height <= 0 {
		return nil
	}
	return m.pv.SetSize(max(1, m.width-2), max(1, m.height-4))
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
		case "t":
			m.lineMode = !m.lineMode
			return m, m.rebuildReport()
		case "d", "tab":
			m.dataIdx = (m.dataIdx + 1) % len(pdfdoc.ReportDatasets)
			return m, m.rebuildReport()
		case "c":
			m.themeIdx = (m.themeIdx + 1) % len(pdfdoc.Themes)
			return m, m.rebuildReport()
		case "v":
			m.variant = (m.variant + 1) % 5
			return m, m.rebuildReport()
		case "m":
			return m, m.pv.ToggleMode()
		case "r":
			m.lineMode, m.dataIdx, m.themeIdx, m.variant = false, 0, 0, 0
			fitCmd := m.pv.SetFit(ntpdf.FitContain)
			var modeCmd tea.Cmd
			if m.pv.Mode() != ntpdf.ImageMode {
				modeCmd = m.pv.ToggleMode()
			}
			return m, tea.Batch(fitCmd, modeCmd, m.rebuildReport())
		}
	}

	var cmd tea.Cmd
	m.pv, cmd = m.pv.Update(msg)
	return m, cmd
}

func (m model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("loading…")
	}
	header := headerStyle.Render("LIVE PDF REPORT BUILDER") + "  " +
		footerStyle.Render("t: bar/line · d: dataset · c: theme · v: vary · n/p: page · m: image/text · f: fit · g: glyph/kitty · q: quit")
	body := boxStyle.Render(m.pv.View().Content)
	status := footerStyle.Width(m.width).Render(m.Summary())
	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, header, body, status))
	v.AltScreen = true
	return v
}

func main() { demohelp.Main(newModel()) }
