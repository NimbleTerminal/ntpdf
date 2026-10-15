// examples/pagedims/main.go — aspect-matched cell rectangle demo.
//
// Shows how to use ntpdf.Model.PageDimensions and PagePointSize to size the
// widget's cell rectangle to a page's true aspect ratio, so ntpdf.FitContain
// renders the page edge-to-edge with zero letterbox across widescreen (16:9),
// portrait (US Letter), and square (1:1) pages.
package main

import (
	"fmt"
	"os"
	"runtime"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	picture "nimbleterminal.dev/ntpicture"

	"nimbleterminal.dev/ntpdf"
	"nimbleterminal.dev/ntpdf/examples/internal/demohelp"
	"nimbleterminal.dev/ntpdf/examples/internal/pdfdoc"
)

const (
	defaultCellPixelW = 8
	defaultCellPixelH = 16
)

var (
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#8de3b6"))
	footerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
)

type model struct {
	pv          ntpdf.Model
	aspectLock  bool
	preferKitty bool
	width       int
	height      int
	cols        int
	rows        int
}

func newModel(cfg ntpdf.Config) model {
	if runtime.GOOS == "js" {
		cfg.RenderDPI = 120
		cfg.PictureConfig.KittyMedium = picture.KittyMediumSharedMemory
	}
	return model{
		pv:          ntpdf.NewWithConfig(cfg),
		aspectLock:  true,
		preferKitty: true,
	}
}

func (m model) Init() tea.Cmd { return m.pv.Init() }

func (m model) Help() []key.Binding {
	return []key.Binding{
		demohelp.Key("Next page", "n", "]", "pgdown"),
		demohelp.Key("Prev page", "p", "[", "pgup"),
		demohelp.Key("Aspect lock", "a"),
		demohelp.Key("Image / Text", "m", "t"),
		demohelp.Key("Glyph / Kitty", "g"),
		demohelp.Key("Reset", "r"),
		demohelp.Key("Quit", "q", "ctrl+c"),
	}
}

func (m model) Summary() string {
	aspect := "locked"
	if !m.aspectLock {
		aspect = "full"
	}
	mode := "image"
	if m.pv.Mode() == ntpdf.TextMode {
		mode = "text"
	}
	return fmt.Sprintf("PageDims: page=%d aspect=%s mode=%s", m.pv.Page(), aspect, mode)
}

func (m *model) syncRect() tea.Cmd {
	if m.width <= 0 || m.height <= 0 {
		return nil
	}
	cols, rows := m.aspectCellrect()
	if cols == m.cols && rows == m.rows {
		return nil
	}
	m.cols, m.rows = cols, rows
	return m.pv.SetSize(cols, rows)
}

func (m model) aspectCellrect() (cols, rows int) {
	bodyW := max(1, m.width-2)
	bodyH := max(1, m.height-4)
	if !m.aspectLock {
		return bodyW, bodyH
	}

	var srcW, srcH float64
	if pxW, pxH, ok := m.pv.PageDimensions(m.pv.Page()); ok && pxW > 0 && pxH > 0 {
		srcW, srcH = float64(pxW), float64(pxH)
	} else if ptW, ptH, ok := m.pv.PagePointSize(m.pv.Page()); ok && ptW > 0 && ptH > 0 {
		srcW, srcH = ptW, ptH
	} else {
		return bodyW, bodyH
	}

	pageCellAR := (srcW * defaultCellPixelH) / (srcH * defaultCellPixelW)
	bodyCellAR := float64(bodyW) / float64(bodyH)
	if pageCellAR >= bodyCellAR {
		return bodyW, max(1, int(float64(bodyW)/pageCellAR))
	}
	return max(1, int(float64(bodyH)*pageCellAR)), bodyH
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, m.syncRect()

	case tea.KeyMsg:
		if demohelp.Quit(msg) {
			return m, tea.Sequence(m.pv.Dispose(), tea.Quit)
		}
		if key.Matches(msg, m.pv.KeyMap().ToggleRender) {
			m.preferKitty = false
		}
		switch msg.String() {
		case "a":
			m.aspectLock = !m.aspectLock
			return m, m.syncRect()
		case "r":
			m.aspectLock = true
			var modeCmd tea.Cmd
			if m.pv.Mode() != ntpdf.ImageMode {
				modeCmd = m.pv.ToggleMode()
			}
			pageCmd := m.pv.SetPage(1)
			rectCmd := m.syncRect()
			return m, tea.Batch(modeCmd, pageCmd, rectCmd)
		}
	}

	var cmd tea.Cmd
	m.pv, cmd = m.pv.Update(msg)
	if rectCmd := m.syncRect(); rectCmd != nil {
		cmd = tea.Batch(cmd, rectCmd)
	}
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
	header := headerStyle.Render("ASPECT-MATCHED VIEWPORT") + "  " +
		footerStyle.Render("n/p: page (16:9 / Letter / 1:1) · a: aspect lock · m: image/text · g: glyph/kitty · r: reset · q: quit")
	body := boxStyle.Render(m.pv.View().Content)

	dimDetail := fmt.Sprintf("  cellrect=%dx%d", m.cols, m.rows)
	if pageW, pageH, ok := m.pv.PageDimensions(m.pv.Page()); ok {
		dimDetail = fmt.Sprintf("  bitmap=%dx%dpx → cellrect=%dx%d", pageW, pageH, m.cols, m.rows)
	} else if ptW, ptH, ok := m.pv.PagePointSize(m.pv.Page()); ok {
		dimDetail = fmt.Sprintf("  media=%.0fx%.0fpt → cellrect=%dx%d", ptW, ptH, m.cols, m.rows)
	}
	status := footerStyle.Width(m.width).Render(m.Summary() + dimDetail)
	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, header, body, status))
	v.AltScreen = true
	return v
}

func initialConfig() (ntpdf.Config, error) {
	if len(os.Args) > 1 {
		path := os.Args[1]
		if _, err := os.Stat(path); err != nil {
			return ntpdf.Config{}, fmt.Errorf("argv[1] %q: %w", path, err)
		}
		return ntpdf.Config{
			InitialPath: path,
			DefaultMode: ntpdf.ImageMode,
			DynamicDPI:  runtime.GOOS != "js",
		}, nil
	}
	return ntpdf.Config{
		InitialData: pdfdoc.BuildAspectSample(),
		InitialName: "aspect-calibration.pdf",
		DefaultMode: ntpdf.ImageMode,
		DynamicDPI:  runtime.GOOS != "js",
	}, nil
}

func main() {
	cfg, err := initialConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	demohelp.Main(newModel(cfg))
}
