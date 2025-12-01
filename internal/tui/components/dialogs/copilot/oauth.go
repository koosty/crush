package copilot

import (
	"context"
	"log/slog"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/oauth"
	"github.com/charmbracelet/crush/internal/oauth/copilot"
	"github.com/charmbracelet/crush/internal/tui/styles"
	"github.com/charmbracelet/crush/internal/tui/util"
)

// OAuthState represents the current state of the OAuth flow.
type OAuthState int

const (
	OAuthStateInit OAuthState = iota
	OAuthStateWaitingForAuth
	OAuthStateValidating
	OAuthStateSuccess
	OAuthStateError
)

// ValidationCompletedMsg is sent when token validation completes.
type ValidationCompletedMsg struct {
	Token string
	Error error
}

// AuthenticationCompleteMsg is sent when authentication is complete.
type AuthenticationCompleteMsg struct{}

// OAuth2 represents the GitHub Copilot OAuth device flow dialog.
type OAuth2 struct {
	State        OAuthState
	width        int
	isOnboarding bool

	// Device flow state.
	deviceCode      string
	userCode        string
	verificationURI string
	interval        int
	err             error
	token           string

	// UI components.
	spinner    spinner.Model
	cancelFunc context.CancelFunc
}

// NewOAuth2 creates a new OAuth2 dialog for GitHub Copilot.
func NewOAuth2() *OAuth2 {
	return &OAuth2{
		State: OAuthStateInit,
	}
}

// Init initializes the OAuth component UI (spinner only).
// Call StartFlow() to actually begin the device flow.
func (o *OAuth2) Init() tea.Cmd {
	t := styles.CurrentTheme()

	o.spinner = spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(t.S().Base.Foreground(t.Green)),
	)

	return o.spinner.Tick
}

// StartFlow begins the OAuth device flow. Call this when the user
// selects GitHub Copilot as their provider.
func (o *OAuth2) StartFlow() tea.Cmd {
	// Reset state in case this is a retry.
	o.SetDefaults()
	o.State = OAuthStateInit

	// Re-initialize spinner.
	t := styles.CurrentTheme()
	o.spinner = spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(t.S().Base.Foreground(t.Green)),
	)

	// Start the device flow.
	return tea.Batch(
		o.spinner.Tick,
		o.startDeviceFlow,
	)
}

func (o *OAuth2) startDeviceFlow() tea.Msg {
	slog.Info("Copilot OAuth: Starting device flow")
	resp, err := copilot.StartDeviceFlow(context.Background())
	if err != nil {
		slog.Error("Copilot OAuth: Device flow failed", "error", err)
		return ValidationCompletedMsg{Error: err}
	}

	slog.Info("Copilot OAuth: Device flow started",
		"user_code", resp.UserCode,
		"verification_uri", resp.VerificationURI,
		"interval", resp.Interval)

	return DeviceFlowStartedMsg{
		DeviceCode:      resp.DeviceCode,
		UserCode:        resp.UserCode,
		VerificationURI: resp.VerificationURI,
		Interval:        resp.Interval,
	}
}

// DeviceFlowStartedMsg is sent when the device flow has started successfully.
type DeviceFlowStartedMsg struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	Interval        int
}

// Update handles messages for the OAuth dialog.
func (o *OAuth2) Update(msg tea.Msg) (util.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case DeviceFlowStartedMsg:
		slog.Info("Copilot OAuth: Received DeviceFlowStartedMsg",
			"user_code", msg.UserCode,
			"verification_uri", msg.VerificationURI)
		o.deviceCode = msg.DeviceCode
		o.userCode = msg.UserCode
		o.verificationURI = msg.VerificationURI
		o.interval = msg.Interval
		o.State = OAuthStateWaitingForAuth

		// Start polling immediately - user will open browser manually.
		ctx, cancel := context.WithCancel(context.Background())
		o.cancelFunc = cancel
		cmds = append(cmds, o.spinner.Tick, o.pollForToken(ctx))

	case ValidationCompletedMsg:
		slog.Info("Copilot OAuth: Received ValidationCompletedMsg", "error", msg.Error)
		if msg.Error != nil {
			o.err = msg.Error
			o.State = OAuthStateError
		} else {
			o.token = msg.Token
			o.State = OAuthStateSuccess
		}

	case PollingResultMsg:
		slog.Info("Copilot OAuth: Received PollingResultMsg", "has_token", msg.Token != "", "error", msg.Error)
		if msg.Error != nil {
			o.err = msg.Error
			o.State = OAuthStateError
		} else if msg.Token != "" {
			o.token = msg.Token
			o.State = OAuthStateSuccess
		}
		// If no error and no token, keep polling (handled in polling goroutine).
	}

	// Update spinner for states that need animation.
	if o.State == OAuthStateInit || o.State == OAuthStateWaitingForAuth || o.State == OAuthStateValidating {
		var cmd tea.Cmd
		o.spinner, cmd = o.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}

	return o, tea.Batch(cmds...)
}

// ValidationConfirm is called when the user presses Enter.
func (o *OAuth2) ValidationConfirm() (util.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch o.State {
	case OAuthStateInit, OAuthStateWaitingForAuth:
		// Still waiting, do nothing.
		return o, nil

	case OAuthStateSuccess:
		cmds = append(cmds, func() tea.Msg { return AuthenticationCompleteMsg{} })

	case OAuthStateError:
		// Reset and try again.
		o.SetDefaults()
		cmds = append(cmds, o.spinner.Tick, o.startDeviceFlow)
	}

	return o, tea.Batch(cmds...)
}

// PollingResultMsg is sent when polling for token completes.
type PollingResultMsg struct {
	Token string
	Error error
}

func (o *OAuth2) pollForToken(ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		slog.Info("Copilot OAuth: Starting polling", "device_code", o.deviceCode[:8]+"...", "interval", o.interval)
		token, err := copilot.PollForToken(ctx, o.deviceCode, o.interval)
		slog.Info("Copilot OAuth: Polling completed", "has_token", token != "", "error", err)
		return PollingResultMsg{Token: token, Error: err}
	}
}

// View renders the OAuth dialog.
func (o *OAuth2) View() string {
	t := styles.CurrentTheme()

	whiteStyle := lipgloss.NewStyle().Foreground(t.White)
	primaryStyle := lipgloss.NewStyle().Foreground(t.Primary)
	successStyle := lipgloss.NewStyle().Foreground(t.Success)
	errorStyle := lipgloss.NewStyle().Foreground(t.Error)
	mutedStyle := lipgloss.NewStyle().Foreground(t.FgMuted)

	titleStyle := whiteStyle
	if o.isOnboarding {
		titleStyle = primaryStyle
	}

	switch o.State {
	case OAuthStateInit:
		// Still loading device flow.
		return lipgloss.NewStyle().
			Margin(0, 1).
			Render(o.spinner.View() + " " + titleStyle.Render("Starting GitHub authentication..."))

	case OAuthStateWaitingForAuth:
		heading := lipgloss.NewStyle().
			Margin(0, 1).
			Render(o.spinner.View() + " " + titleStyle.Render("Waiting for authorization..."))

		urlLine := lipgloss.NewStyle().
			Margin(1, 1).
			Render(titleStyle.Render("Open: ") + successStyle.Render(o.verificationURI))

		codeBox := lipgloss.NewStyle().
			Margin(1, 2).
			Padding(1, 3).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(t.Primary).
			Render(successStyle.Bold(true).Render(o.userCode))

		instructions := lipgloss.NewStyle().
			Margin(0, 1).
			Render(mutedStyle.Render("Enter this code on GitHub to authorize"))

		return lipgloss.JoinVertical(
			lipgloss.Left,
			heading,
			urlLine,
			codeBox,
			instructions,
		)

	case OAuthStateValidating:
		return lipgloss.NewStyle().
			Margin(0, 1).
			Render(o.spinner.View() + " " + titleStyle.Render("Validating token..."))

	case OAuthStateSuccess:
		return lipgloss.NewStyle().
			Margin(0, 1).
			Render(styles.CheckIcon + " " + successStyle.Render("GitHub Copilot authenticated successfully!") + "\n\n" +
				mutedStyle.Render("Press Enter to continue"))

	case OAuthStateError:
		errMsg := "Unknown error"
		if o.err != nil {
			errMsg = o.err.Error()
		}
		return lipgloss.JoinVertical(
			lipgloss.Left,
			lipgloss.NewStyle().
				Margin(0, 1).
				Render(styles.ErrorIcon+" "+errorStyle.Render("Authentication failed")),
			lipgloss.NewStyle().
				Margin(1, 1).
				Render(mutedStyle.Render(errMsg)),
			lipgloss.NewStyle().
				Margin(1, 1).
				Render(mutedStyle.Render("Press Enter to try again")),
		)

	default:
		return ""
	}
}

// SetDefaults resets the dialog to its initial state.
func (o *OAuth2) SetDefaults() {
	if o.cancelFunc != nil {
		o.cancelFunc()
		o.cancelFunc = nil
	}
	o.State = OAuthStateInit
	o.deviceCode = ""
	o.userCode = ""
	o.verificationURI = ""
	o.interval = 0
	o.err = nil
	o.token = ""
}

// SetWidth sets the dialog width.
func (o *OAuth2) SetWidth(w int) {
	o.width = w
}

// SetError sets an error state.
func (o *OAuth2) SetError(err error) {
	o.err = err
	o.State = OAuthStateError
}

// Token returns the obtained OAuth token as an oauth.Token.
func (o *OAuth2) Token() *oauth.Token {
	if o.token == "" {
		return nil
	}
	// For Copilot, the GitHub OAuth token is stored as RefreshToken
	// because it's used to obtain short-lived Copilot API tokens.
	return &oauth.Token{
		RefreshToken: o.token,
	}
}
