package components

import (
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jorgerojas26/lazysql/app"
	"github.com/jorgerojas26/lazysql/commands"
	"github.com/jorgerojas26/lazysql/helpers/logger"
	"github.com/jorgerojas26/lazysql/internal/copilot"
)

// CopilotPane is a chat pane that talks to GitHub Copilot Chat. It is aware of
// the current connection/schema, can receive context from the SQL editor, and
// can push suggested SQL back into the editor. It never executes modifying SQL.
type CopilotPane struct {
	*tview.Flex

	transcript *tview.TextView
	input      *tview.TextArea
	status     *tview.TextView

	home     *Home
	service  *copilotService
	messages []copilot.Message
	lastSQL  []string
	busy     bool
}

// NewCopilotPane builds the Copilot pane bound to the given Home.
func NewCopilotPane(home *Home) *CopilotPane {
	transcript := tview.NewTextView()
	transcript.SetDynamicColors(true)
	transcript.SetScrollable(true)
	transcript.SetWordWrap(true)
	transcript.SetBorder(true)
	transcript.SetTitle(" Copilot ")
	transcript.SetTitleAlign(tview.AlignLeft)

	status := tview.NewTextView()
	status.SetDynamicColors(true)
	status.SetTextColor(app.Styles.TertiaryTextColor)

	input := tview.NewTextArea()
	input.SetBorder(true)
	input.SetTitle(" Prompt (Ctrl+R send · Ctrl+Y insert · Ctrl+E run SELECT · Ctrl+O settings) ")
	input.SetTitleAlign(tview.AlignLeft)

	flex := tview.NewFlex().SetDirection(tview.FlexRow)
	flex.AddItem(transcript, 0, 1, false)
	flex.AddItem(status, 1, 0, false)
	flex.AddItem(input, 6, 0, true)

	pane := &CopilotPane{
		Flex:       flex,
		transcript: transcript,
		input:      input,
		status:     status,
		home:       home,
		service:    getCopilotService(),
	}

	pane.SetInputCapture(pane.inputCapture)
	pane.refreshStatus()
	pane.appendSystemLine("Welcome to Copilot. Ask about your data or for SQL help. Modifying SQL is only ever provided as text for you to review and run manually.")

	return pane
}

// notifyDisabled informs the user that Copilot is toggled off in settings.
func (p *CopilotPane) notifyDisabled() {
	p.appendSystemLine("Copilot is disabled in settings. Press Ctrl+O to open settings and enable it.")
	p.refreshStatus()
}

// GetPrimitive satisfies the pane/tab content contract.
func (p *CopilotPane) GetPrimitive() tview.Primitive {
	return p.Flex
}

// FocusInput moves focus to the prompt input.
func (p *CopilotPane) FocusInput() {
	app.App.SetFocus(p.input)
}

func (p *CopilotPane) refreshStatus() {
	cfg := copilotConfig()
	if !p.service.IsAuthenticated() {
		p.status.SetText("[yellow]Not logged in — press Ctrl+O to open settings and log in with GitHub or a PAT.")
		return
	}
	rowState := "row data OFF"
	if cfg.AllowRowData {
		rowState = fmt.Sprintf("row data ON (max %d)", cfg.MaxRows)
	}
	p.status.SetText(fmt.Sprintf("[green]Ready[white] · model %s · %s", cfg.Model, rowState))
}

func (p *CopilotPane) appendSystemLine(text string) {
	fmt.Fprintf(p.transcript, "[gray]%s[white]\n\n", tview.Escape(text))
	p.transcript.ScrollToEnd()
}

func (p *CopilotPane) appendUser(text string) {
	fmt.Fprintf(p.transcript, "[aqua]You:[white] %s\n\n", tview.Escape(text))
	p.transcript.ScrollToEnd()
}

func (p *CopilotPane) appendAssistant(text string) {
	fmt.Fprintf(p.transcript, "[green]Copilot:[white] %s\n\n", tview.Escape(text))
	p.transcript.ScrollToEnd()
}

func (p *CopilotPane) appendError(text string) {
	fmt.Fprintf(p.transcript, "[red]Error:[white] %s\n\n", tview.Escape(text))
	p.transcript.ScrollToEnd()
}

// AddContext appends a context block to the conversation (not shown verbatim in
// the transcript) so the next prompt is answered with that context.
func (p *CopilotPane) AddContext(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	p.messages = append(p.messages, copilot.Message{Role: copilot.RoleUser, Content: text})
	p.appendSystemLine("Context from the current connection was attached to the conversation.")
}

func (p *CopilotPane) inputCapture(event *tcell.EventKey) *tcell.EventKey {
	command := app.Keymaps.Group(app.CopilotGroup).Resolve(event)

	switch command {
	case commands.CopilotSend:
		p.send()
		return nil
	case commands.InsertCopilotSQL:
		p.insertSQL(false)
		return nil
	case commands.CopilotRunSQL:
		p.insertSQL(true)
		return nil
	case commands.OpenCopilotConfig:
		ShowCopilotConfigModal(p)
		return nil
	case commands.UnfocusEditor:
		if p.home != nil {
			p.home.focusRightWrapper()
		}
		return nil
	}

	return event
}

func (p *CopilotPane) send() {
	if p.busy {
		return
	}
	prompt := strings.TrimSpace(p.input.GetText())
	if prompt == "" {
		return
	}
	if !p.service.IsAuthenticated() {
		p.appendError("Not logged in. Press Ctrl+O to open settings and authenticate.")
		return
	}

	p.input.SetText("", false)
	p.appendUser(prompt)
	p.messages = append(p.messages, copilot.Message{Role: copilot.RoleUser, Content: prompt})

	p.busy = true
	p.status.SetText("[yellow]Thinking…")

	messages := make([]copilot.Message, len(p.messages))
	copy(messages, p.messages)

	go func() {
		ctx := app.App.Context()
		reply, err := p.service.Ask(ctx, messages)

		app.App.QueueUpdateDraw(func() {
			p.busy = false
			p.refreshStatus()
			if err != nil {
				logger.Error("Copilot request failed", map[string]any{"error": err.Error()})
				p.appendError(err.Error())
				return
			}
			p.messages = append(p.messages, copilot.Message{Role: copilot.RoleAssistant, Content: reply})
			p.appendAssistant(reply)
			p.lastSQL = copilot.ExtractSQLBlocks(reply)
			if len(p.lastSQL) > 0 {
				p.appendSystemLine("A SQL suggestion was detected. Ctrl+Y inserts it into the editor; Ctrl+E inserts & runs it (SELECT only, with confirmation).")
			}
		})
	}()
}

// insertSQL inserts the most recent SQL suggestion into the editor. When run is
// true and the SQL is a read-only SELECT, it prompts for confirmation and then
// executes it. Modifying SQL is never executed — it is inserted as text only.
func (p *CopilotPane) insertSQL(run bool) {
	if len(p.lastSQL) == 0 {
		p.appendSystemLine("No SQL suggestion available yet.")
		return
	}
	sql := p.lastSQL[len(p.lastSQL)-1]

	if p.home == nil {
		return
	}
	p.home.createOrFocusEditorTab()
	tab := p.home.TabbedPane.GetCurrentTab()
	if tab == nil {
		return
	}
	table, ok := tab.Content.(*ResultsTable)
	if !ok || table.Editor == nil {
		return
	}
	table.Editor.SetText(sql, true)

	if !run {
		p.appendSystemLine("Inserted SQL into the editor. Review it before running.")
		return
	}

	if copilot.IsModifying(sql) {
		p.appendSystemLine("This is a modifying statement, so it was inserted as text only. Review and run it manually from the editor.")
		return
	}

	// SELECT / read-only: confirm before executing.
	modal := NewConfirmationModal("Run this SELECT query suggested by Copilot?")
	modal.SetDoneFunc(func(_ int, label string) {
		mainPages.RemovePage(pageNameConfirmation)
		if label == confirmationYes {
			table.Editor.Publish(eventSQLEditorQuery, sql)
		} else {
			app.App.SetFocus(table.Editor)
		}
	})
	mainPages.AddPage(pageNameConfirmation, modal, true, true)
	app.App.SetFocus(modal)
}

// CopyLastSQL copies the most recent SQL suggestion to the clipboard.
func (p *CopilotPane) CopyLastSQL() {
	if len(p.lastSQL) == 0 {
		return
	}
	if err := clipboard.WriteAll(p.lastSQL[len(p.lastSQL)-1]); err != nil {
		logger.Error("Failed to copy SQL to clipboard", map[string]any{"error": err.Error()})
	}
}
