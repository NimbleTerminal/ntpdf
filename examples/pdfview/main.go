// examples/pdfview/main.go — single-pane ntpdf demo.
//
// Loads the PDF named on the command line (defaulting to the embedded
// testdata/Example.pdf when no path is given) and renders it. Keys:
//
//	n/→/l       next page
//	p/←/h       prev page
//	m or t      toggle Text ↔ Image mode
//	g           toggle Glyph ↔ Kitty (Image mode only)
//	r           reload the current page (Image mode only)
//	?           toggle help
//	q / ctrl+c  quit
package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"nimbleterminal.dev/ntpdf"
	"nimbleterminal.dev/ntpdf/examples/internal/demohelp"
	picture "nimbleterminal.dev/ntpicture"
)

// embeddedExample is the demo PDF compiled into the binary, used when
// the user runs the example without a path argument. Sized small (~270KB)
// so the binary stays reasonable. The same file is the canonical
// fixture for ntpdf's integration tests.
//
//go:embed testdata/Example.pdf
var embeddedExample []byte

var (
	boxStyle       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
	footerStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	errStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	badgeOkStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	badgeOffStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	badgeWarnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
)

// kittyBadge renders a compact indicator for the terminal's Kitty graphics
// capability. The probe takes a few ms to resolve so the initial state is
// "kitty:?".
func kittyBadge(cap picture.KittyCapability) string {
	switch cap {
	case picture.KittyCapabilitySupported:
		return badgeOkStyle.Render("kitty:✓")
	case picture.KittyCapabilityUnsupported:
		return badgeOffStyle.Render("kitty:✗")
	default:
		return badgeOffStyle.Render("kitty:?")
	}
}

// appKeys is the parent program's help / status surface. The ntpdf widget
// owns the page/mode bindings; this struct only exists so the bubbles/help
// bubble has a single KeyMap to render from.
type appKeys struct {
	nav    key.Binding
	mode   key.Binding
	render key.Binding
	reload key.Binding
	zoom   key.Binding
	pan    key.Binding
	reset  key.Binding
	fit    key.Binding
	help   key.Binding
	quit   key.Binding
}

func newAppKeys() appKeys {
	return appKeys{
		nav:    key.NewBinding(key.WithKeys("n", "p", "h", "l"), key.WithHelp("n/p", "page")),
		mode:   key.NewBinding(key.WithKeys("m", "t"), key.WithHelp("m", "text/image")),
		render: key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "glyph/kitty")),
		reload: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reload")),
		zoom:   key.NewBinding(key.WithKeys("+", "-"), key.WithHelp("+/-", "zoom")),
		pan:    key.NewBinding(key.WithKeys("left", "right", "up", "down"), key.WithHelp("←↑↓→", "pan (zoomed)")),
		reset:  key.NewBinding(key.WithKeys("0"), key.WithHelp("0", "reset")),
		fit:    key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "fit mode")),
		help:   key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "more")),
		quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

func (k appKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.nav, k.zoom, k.pan, k.mode, k.fit, k.help, k.quit}
}

func (k appKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.nav, k.mode, k.render},
		{k.zoom, k.pan, k.reset},
		{k.fit, k.reload, k.help, k.quit},
	}
}

type model struct {
	pv            ntpdf.Model
	help          help.Model
	keys          appKeys
	width, height int
	preferKitty   bool
}

func initialModel(cfg ntpdf.Config) model {
	if runtime.GOOS == "js" {
		cfg.DefaultMode = ntpdf.ImageMode
		cfg.PictureConfig.KittyMedium = picture.KittyMediumSharedMemory
	}
	return model{
		pv:          ntpdf.NewWithConfig(cfg),
		help:        help.New(),
		keys:        newAppKeys(),
		preferKitty: true,
	}
}

func (m model) Init() tea.Cmd { return m.pv.Init() }

func (m model) Help() []key.Binding {
	return []key.Binding{
		demohelp.Key("Next page", "n", "l"),
		demohelp.Key("Prev page", "p", "h"),
		demohelp.Key("Text / Image", "m", "t"),
		demohelp.Key("Fit mode", "f"),
		demohelp.Key("Zoom in", "+", "="),
		demohelp.Key("Zoom out", "-", "_"),
		demohelp.Key("Glyph / Kitty", "g"),
		demohelp.Key("Reset view", "0"),
		demohelp.Key("Quit", "q", "ctrl+c"),
	}
}

func (m model) Summary() string {
	mode := "text"
	if m.pv.Mode() == ntpdf.ImageMode {
		mode = "image"
	}
	fit := "contain"
	switch m.pv.Fit() {
	case ntpdf.FitFill:
		fit = "fill"
	case ntpdf.FitCover:
		fit = "cover"
	}
	return fmt.Sprintf("Viewer: name=%s page=%d/%d mode=%s fit=%s zoom=%dx",
		filepath.Base(m.pv.Name()), m.pv.Page(), m.pv.NumPages(), mode, fit, 1<<m.pv.Zoom())
}

func (m *model) resize() tea.Cmd {
	if m.width == 0 || m.height == 0 {
		return nil
	}
	m.help.SetWidth(m.width)
	// Help bubble height swings between 1 (short) and 3 (FullHelp) rows;
	// measure it after toggling so the viewer shrinks instead of
	// scrolling the top of the box off-screen.
	helpH := lipgloss.Height(m.help.View(m.keys))
	innerW := m.width - 2
	innerH := m.height - helpH - 1 /* footer */ - 2 /* box borders */
	if innerW < 1 {
		innerW = 1
	}
	if innerH < 1 {
		innerH = 1
	}
	return m.pv.SetSize(innerW, innerH)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, m.resize()

	case tea.KeyMsg:
		if key.Matches(msg, m.pv.KeyMap().ToggleRender) {
			m.preferKitty = false
		}
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Sequence(m.pv.Dispose(), tea.Quit)
		case "?":
			m.help.ShowAll = !m.help.ShowAll
			return m, m.resize()

		// Modal arrow-key policy lives here, not in the widget:
		// "arrow at zoom 0 = page nav" is host-specific. When the
		// widget is actually zoomed, fall through to the widget so
		// its KeyMap dispatches the pan action.
		case "left":
			if m.pv.Mode() != ntpdf.ImageMode || m.pv.Zoom() == 0 {
				return m, m.pv.PrevPage()
			}
		case "right":
			if m.pv.Mode() != ntpdf.ImageMode || m.pv.Zoom() == 0 {
				return m, m.pv.NextPage()
			}
		}
	}

	// All other key + non-key messages flow through the widget. The
	// widget's Update routes KeyMsg through its KeyMap, calling the
	// matching action methods (NextPage / ToggleMode / ZoomIn / etc.).

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
		v := tea.NewView("loading…")
		v.AltScreen = true
		return v
	}
	// picture.Picture embeds Kitty placement escapes directly inside View(),
	// so wrapping the content in a lipgloss border preserves them. Glyph mode
	// just contains styled half-block chars and composes cleanly.
	body := boxStyle.Render(m.pv.View().Content)

	modeName := "Text"
	if m.pv.Mode() == ntpdf.ImageMode {
		modeName = "Image"
		if !m.pv.HasRenderer() {
			// ImageMode is selected but no Renderer is attached — either
			// no factory configured (typical on wasip1/wasm), or the factory
			// errored. Surface the specific cause when we have one so
			// the user understands why pressing 'm' produced no change.
			// Sanitize the error text: factory errors can echo back
			// hostile filenames or PDF-derived bytes.
			label := "Image (no renderer)"
			if err := m.pv.RendererErr(); err != nil {
				label = fmt.Sprintf("Image (renderer error: %s)", ntpdf.SanitizeForTerminal(err.Error()))
			}
			modeName = badgeWarnStyle.Render(label)
		}
	}
	renderName := "Glyph"
	if m.pv.RenderMode() == ntpdf.RenderKitty {
		renderName = "Kitty"
	}
	zoomTag := ""
	if z := m.pv.Zoom(); z > 0 {
		zoomTag = fmt.Sprintf("  ×%d", 1<<z)
	}
	fitName := "Contain"
	switch m.pv.Fit() {
	case ntpdf.FitFill:
		fitName = "Fill"
	case ntpdf.FitCover:
		fitName = "Cover"
	}
	// filepath.Base trims directory components from argv paths while
	// leaving in-memory display names (e.g. "Example.pdf" from the
	// embedded fixture) unchanged. Sanitize before display: PDF paths
	// can carry control / bidi format chars from the OS or the embed.
	name := ntpdf.SanitizeForTerminal(filepath.Base(m.pv.Name()))
	status := fmt.Sprintf("%s  page %d/%d%s  %s  %s  fit:%s  %s",
		name, m.pv.Page(), m.pv.NumPages(), zoomTag, modeName, renderName, fitName,
		kittyBadge(m.pv.KittySupported()))
	if err := m.pv.Err(); err != nil {
		status = errStyle.Render(ntpdf.SanitizeForTerminal(err.Error())) + "  " + status
	}
	footer := footerStyle.Width(m.width).Render(status)

	out := lipgloss.JoinVertical(lipgloss.Left, body, footer, m.help.View(m.keys))
	v := tea.NewView(out)
	v.AltScreen = true
	return v
}

func main() {
	cfg, err := initialConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	demohelp.Main(initialModel(cfg))
}

// initialConfig assembles the ntpdf.Config the example launches with.
// argv[1] wins as a filesystem path; otherwise the embedded
// Example.pdf bytes are passed in directly via Config.InitialData —
// no temp file, no os.ReadFile, no filesystem entanglement.
//
// The browser build (GOOS=js GOARCH=wasm) ships with a fixed wasm heap
// inside @embedpdf/pdfium; 300 DPI on a Letter page wants ~34 MB just
// for the BGRA bitmap, which can push pdfium's auto-grown heap past
// where it copes well. browserDefaultRenderDPI() returns a tighter
// default for that path; native builds keep the package default (300).
func initialConfig() (ntpdf.Config, error) {
	dpi := browserDefaultRenderDPI()
	if len(os.Args) > 1 {
		path := os.Args[1]
		if _, err := os.Stat(path); err != nil {
			return ntpdf.Config{}, fmt.Errorf("argv[1] %q: %w", path, err)
		}
		return ntpdf.Config{InitialPath: path, RenderDPI: dpi}, nil
	}
	return ntpdf.Config{
		InitialData: embeddedExample,
		InitialName: "Example.pdf",
		RenderDPI:   dpi,
	}, nil
}
