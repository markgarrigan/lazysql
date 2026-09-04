package components

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/pkg/browser"
	"github.com/rivo/tview"

	"github.com/jorgerojas26/lazysql/app"
	"github.com/jorgerojas26/lazysql/helpers/logger"
	"github.com/jorgerojas26/lazysql/internal/copilot"
	"github.com/jorgerojas26/lazysql/models"
)

const pageNameCopilotConfig = "CopilotConfig"
const pageNameCopilotDevice = "CopilotDevice"

// ShowCopilotConfigModal presents the Copilot settings form. pane may be nil
// when opened outside of the Copilot pane (e.g. from the connections view).
func ShowCopilotConfigModal(pane *CopilotPane) {
	if mainPages == nil {
		return
	}

	service := getCopilotService()
	cfg := copilotConfig()

	form := tview.NewForm().
		SetFieldBackgroundColor(app.Styles.InverseTextColor).
		SetButtonBackgroundColor(app.Styles.InverseTextColor).
		SetLabelColor(app.Styles.PrimaryTextColor).
		SetFieldTextColor(app.Styles.ContrastSecondaryTextColor)

	authMethods := []string{copilot.AuthMethodDevice, copilot.AuthMethodPAT}
	authIndex := 0
	if cfg.AuthMethod == copilot.AuthMethodPAT {
		authIndex = 1
	}

	form.AddCheckbox("Enabled", cfg.Enabled, nil)
	form.AddDropDown("Auth method", authMethods, authIndex, nil)
	form.AddInputField("Model", cfg.Model, 0, nil, nil)
	form.AddCheckbox("Allow row data (privacy)", cfg.AllowRowData, nil)
	form.AddInputField("Max rows", strconv.Itoa(cfg.MaxRows), 0, nil, nil)
	form.AddPasswordField("PAT (for PAT auth)", "", 0, '*', nil)

	status := tview.NewTextView()
	status.SetDynamicColors(true)
	if service.IsAuthenticated() {
		status.SetText("[green]Authenticated.")
	} else {
		status.SetText("[yellow]Not authenticated.")
	}

	// readForm extracts the current form values into a config struct.
	readForm := func() (enabled, allowRows bool, method, model string, maxRows int, pat string) {
		enabled = form.GetFormItemByLabel("Enabled").(*tview.Checkbox).IsChecked()
		_, method = form.GetFormItemByLabel("Auth method").(*tview.DropDown).GetCurrentOption()
		model = strings.TrimSpace(form.GetFormItemByLabel("Model").(*tview.InputField).GetText())
		allowRows = form.GetFormItemByLabel("Allow row data (privacy)").(*tview.Checkbox).IsChecked()
		maxRows, _ = strconv.Atoi(strings.TrimSpace(form.GetFormItemByLabel("Max rows").(*tview.InputField).GetText()))
		pat = strings.TrimSpace(form.GetFormItemByLabel("PAT (for PAT auth)").(*tview.InputField).GetText())
		return
	}

	saveConfig := func() {
		enabled, allowRows, method, model, maxRows, _ := readForm()
		appCfg := app.App.Config()
		appCfg.Copilot = copilot.Normalize(newCopilotConfig(enabled, method, model, allowRows, maxRows))
		if err := app.App.SaveAppConfig(); err != nil {
			logger.Error("Failed to save Copilot config", map[string]any{"error": err.Error()})
			status.SetText("[red]Failed to save config: " + err.Error())
			return
		}
		if pane != nil {
			pane.refreshStatus()
		}
		status.SetText("[green]Settings saved.")
	}

	form.AddButton("Save", saveConfig)

	form.AddButton("Log in with GitHub", func() {
		saveConfig()
		startCopilotDeviceLogin(service, status, pane)
	})

	form.AddButton("Save PAT", func() {
		_, _, _, _, _, pat := readForm()
		if pat == "" {
			status.SetText("[red]Enter a PAT first.")
			return
		}
		if err := service.SavePAT(pat); err != nil {
			status.SetText("[red]Failed to save PAT: " + err.Error())
			return
		}
		saveConfig()
		status.SetText("[green]PAT saved and authenticated.")
		if pane != nil {
			pane.refreshStatus()
		}
	})

	form.AddButton("Log out", func() {
		if err := service.Logout(); err != nil {
			status.SetText("[red]Failed to log out: " + err.Error())
			return
		}
		status.SetText("[yellow]Logged out.")
		if pane != nil {
			pane.refreshStatus()
		}
	})

	form.AddButton("Close", func() {
		mainPages.RemovePage(pageNameCopilotConfig)
	})

	form.SetBorder(true)
	form.SetTitle(" GitHub Copilot Settings ")
	form.SetTitleAlign(tview.AlignLeft)

	form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			mainPages.RemovePage(pageNameCopilotConfig)
			return nil
		}
		return event
	})

	wrapper := tview.NewFlex().SetDirection(tview.FlexRow)
	wrapper.AddItem(form, 0, 1, true)
	wrapper.AddItem(status, 1, 0, false)

	modal := center(wrapper, 60, 20)
	mainPages.AddPage(pageNameCopilotConfig, modal, true, true)
	app.App.SetFocus(form)
}

// startCopilotDeviceLogin runs the device authorization flow: it displays the
// user code and verification URL, opens the browser, and polls for the token.
func startCopilotDeviceLogin(service *copilotService, status *tview.TextView, pane *CopilotPane) {
	ctx := app.App.Context()

	go func() {
		dc, err := service.RequestDeviceCode(ctx)
		if err != nil {
			app.App.QueueUpdateDraw(func() {
				status.SetText("[red]Device login failed: " + err.Error())
			})
			return
		}

		app.App.QueueUpdateDraw(func() {
			showDeviceCodeModal(dc)
		})

		// Best-effort: open the verification URL in the browser.
		openBrowser(dc.VerificationURI)

		err = service.WaitForDeviceToken(ctx, dc)

		app.App.QueueUpdateDraw(func() {
			mainPages.RemovePage(pageNameCopilotDevice)
			if err != nil {
				msg := err.Error()
				if errors.Is(err, copilot.ErrNoCopilotSubscription) {
					msg = "This account does not have an active GitHub Copilot subscription."
				}
				status.SetText("[red]Login failed: " + msg)
				return
			}
			status.SetText("[green]Logged in with GitHub.")
			if pane != nil {
				pane.refreshStatus()
			}
		})
	}()
}

func showDeviceCodeModal(dc *copilot.DeviceCode) {
	text := fmt.Sprintf(
		"To finish signing in to GitHub Copilot:\n\n1. Open: %s\n2. Enter code: [yellow]%s[white]\n\nWaiting for authorization…",
		dc.VerificationURI, dc.UserCode,
	)
	modal := tview.NewModal().
		SetText(text).
		AddButtons([]string{"Cancel"}).
		SetDoneFunc(func(_ int, _ string) {
			mainPages.RemovePage(pageNameCopilotDevice)
		})
	modal.SetBackgroundColor(app.Styles.PrimitiveBackgroundColor)
	mainPages.AddPage(pageNameCopilotDevice, modal, true, true)
	app.App.SetFocus(modal)
}

// newCopilotConfig builds a CopilotConfig from raw form values.
func newCopilotConfig(enabled bool, method, model string, allowRows bool, maxRows int) models.CopilotConfig {
	return models.CopilotConfig{
		Enabled:      enabled,
		AuthMethod:   method,
		Model:        model,
		AllowRowData: allowRows,
		MaxRows:      maxRows,
	}
}

// openBrowser best-effort opens a URL in the user's default browser.
func openBrowser(url string) {
	if err := browser.OpenURL(url); err != nil {
		logger.Info("Could not open browser for Copilot device login", map[string]any{"error": err.Error(), "url": url})
	}
}

// center wraps a primitive in a centered fixed-size grid overlay.
func center(p tview.Primitive, width, height int) tview.Primitive {
	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(p, height, 0, true).
			AddItem(nil, 0, 1, false), width, 0, true).
		AddItem(nil, 0, 1, false)
}
