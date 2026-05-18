package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	modalMinWidth       = 30
	modalMaxWidthRatio  = 0.8
	modalButtonPadding  = 3
	modalMaxStackDepth  = 10
	modalConfirmLabel   = "Yes"
	modalCancelLabel    = "No"
	modalSubmitLabel    = "Submit"
	modalInputCancelLbl = "Cancel"
)

// ---------------------------------------------------------------------------
// ModalResult — message sent when a modal is dismissed
// ---------------------------------------------------------------------------

// ModalResultMsg is sent when a modal is dismissed with a result.
type ModalResultMsg struct {
	ID        string
	Confirmed bool
	Value     string // for InputDialog
}

// ---------------------------------------------------------------------------
// Modal — a single modal overlay
// ---------------------------------------------------------------------------

// Modal is a single modal overlay with title, content, and buttons.
type Modal struct {
	ID           string
	Title        string
	Content      string
	Buttons      []string
	ActiveButton int
	OnResult     func(confirmed bool, value string) tea.Cmd

	// For input modals.
	HasInput bool
	input    textinput.Model
}

// NewModal creates a new modal with the given title and content.
func NewModal(id, title, content string, buttons []string) Modal {
	return Modal{
		ID:      id,
		Title:   title,
		Content: content,
		Buttons: buttons,
	}
}

// View renders the modal box.
func (m Modal) View(termWidth, termHeight int) string {
	boxWidth := int(float64(termWidth) * modalMaxWidthRatio)
	if boxWidth < modalMinWidth {
		boxWidth = modalMinWidth
	}

	// Title.
	titleRendered := modalTitleStyle.Width(boxWidth).Render(m.Title)

	// Content.
	contentRendered := m.Content

	// Input field (if present).
	inputRendered := ""
	if m.HasInput {
		inputRendered = "\n" + m.input.View() + "\n"
	}

	// Buttons.
	var buttonParts []string
	for i, label := range m.Buttons {
		padded := " " + label + " "
		if i == m.ActiveButton {
			buttonParts = append(buttonParts, modalActiveButtonStyle.Render(padded))
		} else {
			buttonParts = append(buttonParts, modalButtonStyle.Render(padded))
		}
		if i < len(m.Buttons)-1 {
			buttonParts = append(buttonParts, "  ")
		}
	}
	buttonsRendered := "\n" + strings.Join(buttonParts, "")

	body := contentRendered + inputRendered + buttonsRendered
	bodyStyled := lipgloss.NewStyle().Width(boxWidth).Render(body)

	box := lipgloss.JoinVertical(lipgloss.Left, titleRendered, bodyStyled)
	styledBox := modalBoxStyle.Render(box)

	// Center on screen.
	boxHeight := lipgloss.Height(styledBox)
	boxWidthActual := lipgloss.Width(styledBox)

	topPad := (termHeight - boxHeight) / 2
	if topPad < 0 {
		topPad = 0
	}
	leftPad := (termWidth - boxWidthActual) / 2
	if leftPad < 0 {
		leftPad = 0
	}

	return lipgloss.NewStyle().
		MarginTop(topPad).
		MarginLeft(leftPad).
		Render(styledBox)
}

// ---------------------------------------------------------------------------
// ModalStack — stackable modal system
// ---------------------------------------------------------------------------

// ModalStack manages a stack of modals, rendering the topmost and routing
// key events to it.
type ModalStack struct {
	stack []Modal
}

// NewModalStack creates a new empty ModalStack.
func NewModalStack() ModalStack {
	return ModalStack{}
}

// Push adds a modal to the top of the stack.
func (ms *ModalStack) Push(m Modal) {
	if len(ms.stack) >= modalMaxStackDepth {
		return
	}
	ms.stack = append(ms.stack, m)
}

// Pop removes and returns the topmost modal. Returns false if the stack
// is empty.
func (ms *ModalStack) Pop() (Modal, bool) {
	if len(ms.stack) == 0 {
		return Modal{}, false
	}
	top := ms.stack[len(ms.stack)-1]
	ms.stack = ms.stack[:len(ms.stack)-1]
	return top, true
}

// IsActive returns true if there are any modals on the stack.
func (ms ModalStack) IsActive() bool {
	return len(ms.stack) > 0
}

// Top returns the topmost modal without removing it. Returns false if
// the stack is empty.
func (ms ModalStack) Top() (Modal, bool) {
	if len(ms.stack) == 0 {
		return Modal{}, false
	}
	return ms.stack[len(ms.stack)-1], true
}

// Update routes a key message to the topmost modal. Returns any command
// produced and whether the modal was dismissed.
func (ms *ModalStack) Update(msg tea.KeyMsg) (tea.Cmd, bool) {
	if len(ms.stack) == 0 {
		return nil, false
	}
	top := &ms.stack[len(ms.stack)-1]

	switch msg.String() {
	case "left", "shift+tab":
		if top.ActiveButton > 0 {
			top.ActiveButton--
		}
		return nil, false

	case "right", "tab":
		if top.ActiveButton < len(top.Buttons)-1 {
			top.ActiveButton++
		}
		return nil, false

	case "enter":
		// Determine if confirmed (first button is confirm).
		confirmed := top.ActiveButton == 0
		value := ""
		if top.HasInput {
			value = top.input.Value()
		}

		var cmd tea.Cmd
		if top.OnResult != nil {
			cmd = top.OnResult(confirmed, value)
		}

		// Also send a result message.
		resultCmd := func() tea.Msg {
			return ModalResultMsg{
				ID:        top.ID,
				Confirmed: confirmed,
				Value:     value,
			}
		}

		ms.stack = ms.stack[:len(ms.stack)-1]
		if cmd != nil {
			return tea.Batch(cmd, resultCmd), true
		}
		return resultCmd, true

	case "esc":
		value := ""
		if top.HasInput {
			value = top.input.Value()
		}
		var cmd tea.Cmd
		if top.OnResult != nil {
			cmd = top.OnResult(false, value)
		}
		resultCmd := func() tea.Msg {
			return ModalResultMsg{
				ID:        top.ID,
				Confirmed: false,
				Value:     value,
			}
		}
		ms.stack = ms.stack[:len(ms.stack)-1]
		if cmd != nil {
			return tea.Batch(cmd, resultCmd), true
		}
		return resultCmd, true

	default:
		// Route to text input if present.
		if top.HasInput {
			var cmd tea.Cmd
			top.input, cmd = top.input.Update(msg)
			return cmd, false
		}
	}

	return nil, false
}

// View renders the topmost modal.
func (ms ModalStack) View(termWidth, termHeight int) string {
	if len(ms.stack) == 0 {
		return ""
	}
	return ms.stack[len(ms.stack)-1].View(termWidth, termHeight)
}

// ---------------------------------------------------------------------------
// ConfirmDialog — simple yes/no confirmation
// ---------------------------------------------------------------------------

// NewConfirmDialog creates a confirmation modal with Yes/No buttons.
func NewConfirmDialog(id, title, message string, onResult func(confirmed bool, value string) tea.Cmd) Modal {
	return Modal{
		ID:       id,
		Title:    title,
		Content:  message,
		Buttons:  []string{modalConfirmLabel, modalCancelLabel},
		OnResult: onResult,
	}
}

// ---------------------------------------------------------------------------
// InputDialog — modal with text input and submit/cancel
// ---------------------------------------------------------------------------

// NewInputDialog creates a modal with a text input field.
func NewInputDialog(id, title, placeholder string, onResult func(confirmed bool, value string) tea.Cmd) Modal {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.CharLimit = 256
	ti.Width = 40
	ti.Focus()

	return Modal{
		ID:       id,
		Title:    title,
		Buttons:  []string{modalSubmitLabel, modalInputCancelLbl},
		HasInput: true,
		input:    ti,
		OnResult: onResult,
	}
}
