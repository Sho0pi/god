package setup

import (
	"bytes"
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mdp/qrterminal/v3"

	"github.com/sho0pi/god/internal/config"
	"github.com/sho0pi/god/internal/connector/whatsapp"
	"github.com/sho0pi/god/internal/godhome"
)

func init() { Register(whatsappWizard{}) }

type whatsappWizard struct{}

func (whatsappWizard) Key() string   { return "whatsapp" }
func (whatsappWizard) Title() string { return "WhatsApp" }

func (whatsappWizard) Enabled(cfg *config.Config) bool {
	return cfg.Connectors.WhatsApp.Enabled
}

func (whatsappWizard) SessionStatus(cfg *config.Config) (bool, string) {
	path, err := storePath(cfg)
	if err != nil {
		return false, "unknown"
	}
	if whatsapp.SessionExists(path) {
		return true, "paired"
	}
	return false, "not paired"
}

func (whatsappWizard) Setup(ctx context.Context, cfg *config.Config, reset bool) (map[string]any, error) {
	path, err := storePath(cfg)
	if err != nil {
		return nil, err
	}

	// The running gateway holds the single-connection sqlite store; opening it
	// here concurrently risks corruption. The gateway lock tells us if it's up.
	release, err := godhome.AcquireGatewayLock()
	if err != nil {
		return nil, fmt.Errorf("the gateway is running — stop it first, then re-run this setup (%w)", err)
	}
	defer release()

	// Already paired and not resetting: nothing to scan, just enable.
	if !reset && whatsapp.SessionExists(path) {
		return map[string]any{"connectors.whatsapp.enabled": true}, nil
	}

	paired, err := runPairing(ctx, path, reset)
	if err != nil {
		return nil, err
	}
	if !paired {
		return nil, fmt.Errorf("pairing cancelled")
	}
	fmt.Println("✓ WhatsApp linked")
	return map[string]any{"connectors.whatsapp.enabled": true}, nil
}

// storePath resolves the WhatsApp session dir: the configured path, else
// ~/.god/whatsapp (mirrors gateway's resolveWhatsAppStore).
func storePath(cfg *config.Config) (string, error) {
	if p := cfg.Connectors.WhatsApp.StorePath; p != "" {
		return p, nil
	}
	return godhome.Path("whatsapp")
}

// --- bubbletea QR screen ---------------------------------------------------

type qrMsg struct{ code string }
type doneMsg struct {
	paired bool
	err    error
}

type qrModel struct {
	cancel context.CancelFunc
	code   string
	status string
	done   bool
	paired bool
	err    error
}

func (m qrModel) Init() tea.Cmd { return nil }

func (m qrModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case qrMsg:
		m.code = msg.code
		m.status = "Scan with WhatsApp → Settings → Linked Devices → Link a Device"
		return m, nil
	case doneMsg:
		m.done = true
		m.paired = msg.paired
		m.err = msg.err
		return m, tea.Quit
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			m.cancel()
			m.status = "cancelling…"
		}
	}
	return m, nil
}

func (m qrModel) View() string {
	if m.done {
		return "" // final status printed by the caller
	}
	var b bytes.Buffer
	b.WriteString("\n")
	if m.code == "" {
		b.WriteString("  Preparing WhatsApp login…\n")
	} else {
		qrterminal.GenerateWithConfig(m.code, qrterminal.Config{
			Level:      qrterminal.L,
			Writer:     &b,
			HalfBlocks: true,
		})
		b.WriteString("\n  " + m.status + "\n")
		b.WriteString("  (the code refreshes automatically; press q to cancel)\n")
	}
	return b.String()
}

// runPairing drives whatsapp.Pair behind a bubbletea screen, returning whether
// the device linked successfully.
func runPairing(ctx context.Context, path string, reset bool) (bool, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	p := tea.NewProgram(qrModel{cancel: cancel, status: "starting…"})

	go func() {
		err := whatsapp.Pair(ctx, path, reset, func(code string) {
			p.Send(qrMsg{code: code})
		})
		p.Send(doneMsg{paired: err == nil, err: err})
	}()

	final, err := p.Run()
	if err != nil {
		return false, err
	}
	m := final.(qrModel)
	if m.err != nil && !errorsIsCancel(m.err, ctx) {
		return false, m.err
	}
	return m.paired, nil
}

// errorsIsCancel reports whether err is just the result of the user cancelling.
func errorsIsCancel(err error, ctx context.Context) bool {
	return ctx.Err() != nil
}
