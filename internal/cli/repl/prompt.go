package repl

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"github.com/fulcrus/hopclaw/acp"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/cli/richedit"
	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

var ErrPromptInterrupted = errors.New("prompt interrupted")
var ErrPromptQuit = errors.New("prompt quit")

const ttyCRLF = "\r\n"

type RichReadResult struct {
	Text          string
	Images        []string
	ContentBlocks []contextengine.ContentBlock
}

type Prompter interface {
	ReadLine(string, *CommandRegistry) (string, error)
	ReadRichLine(string, *CommandRegistry) (RichReadResult, error)
	ReadApproval(string) (rune, error)
	ReadSecret(string) (string, error)
}

type DynamicPrompt struct {
	approval      bool
	paused        bool
	target        string
	stateProvider func() REPLViewState
}

func (p *DynamicPrompt) SetApproval(enabled bool) {
	if p == nil {
		return
	}
	p.approval = enabled
}

func (p *DynamicPrompt) SetTarget(name string) {
	if p == nil {
		return
	}
	p.target = strings.TrimSpace(name)
}

func (p *DynamicPrompt) SetPaused(enabled bool) {
	if p == nil {
		return
	}
	p.paused = enabled
}

func (p *DynamicPrompt) SetStateProvider(provider func() REPLViewState) {
	if p == nil {
		return
	}
	p.stateProvider = provider
}

func (p *DynamicPrompt) Input() string {
	if p != nil && p.approval {
		return "approval> "
	}
	if p != nil && p.paused {
		return "paused> "
	}
	state := REPLViewState{}
	if p != nil && p.stateProvider != nil {
		state = p.stateProvider()
	}
	return workbenchPromptLabel(state) + " "
}

func (p *DynamicPrompt) Approval() string {
	return "approval> "
}

type TerminalPrompter struct {
	in         *os.File
	out        io.Writer
	reader     *bufio.Reader
	tty        bool
	history    *History
	completion richedit.CompletionProvider
	overlay    richedit.OverlayController
	chrome     func(int) richedit.Chrome
}

func NewTerminalPrompter(in *os.File, out io.Writer, history *History) *TerminalPrompter {
	prompter := &TerminalPrompter{
		in:      in,
		out:     out,
		reader:  bufio.NewReader(in),
		history: history,
	}
	if in != nil {
		prompter.tty = term.IsTerminal(int(in.Fd()))
	}
	return prompter
}

func (p *TerminalPrompter) SetCompletionProvider(provider richedit.CompletionProvider) {
	if p == nil {
		return
	}
	p.completion = provider
}

func (p *TerminalPrompter) SetOverlayController(controller richedit.OverlayController) {
	if p == nil {
		return
	}
	p.overlay = controller
}

func (p *TerminalPrompter) SetChromeProvider(provider func(int) richedit.Chrome) {
	if p == nil {
		return
	}
	p.chrome = provider
}

func (p *TerminalPrompter) ReadLine(prompt string, registry *CommandRegistry) (string, error) {
	if !p.tty {
		fmt.Fprint(p.out, prompt)
		line, err := p.reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" && errors.Is(err, io.EOF) {
			return "", io.EOF
		}
		return line, nil
	}

	state, err := term.MakeRaw(int(p.in.Fd()))
	if err != nil {
		return "", err
	}
	defer term.Restore(int(p.in.Fd()), state)

	buffer := []rune{}
	renderedHints := 0
	render := func() {
		clearPromptRender(p.out, renderedHints)
		fmt.Fprint(p.out, prompt, string(buffer))
		hints := []acp.Command(nil)
		if registry != nil {
			hints = registry.Suggestions("/" + strings.TrimPrefix(string(buffer), "/"))
		}
		if !strings.HasPrefix(string(buffer), "/") || registry == nil {
			hints = nil
		}
		for _, hint := range hints {
			fmt.Fprintf(p.out, "%s  /%-12s %s", ttyCRLF, hint.Name, defaultString(registry.HintFor(hint.Name), hint.Description))
		}
		renderedHints = len(hints)
		if renderedHints > 0 {
			width := runewidth.StringWidth(prompt + string(buffer))
			fmt.Fprintf(p.out, "\033[%dA\r\033[%dC", renderedHints, width)
		}
	}

	render()
	for {
		r, _, err := p.reader.ReadRune()
		if err != nil {
			return "", err
		}
		switch r {
		case '\r', '\n':
			clearPromptRender(p.out, renderedHints)
			fmt.Fprint(p.out, prompt, string(buffer), ttyCRLF)
			return string(buffer), nil
		case 3:
			clearPromptRender(p.out, renderedHints)
			fmt.Fprint(p.out, ttyCRLF)
			return "", ErrPromptInterrupted
		case 4:
			if len(buffer) == 0 {
				clearPromptRender(p.out, renderedHints)
				fmt.Fprint(p.out, ttyCRLF)
				return "", io.EOF
			}
		case 12:
			fmt.Fprint(p.out, "\033[2J\033[H")
			renderedHints = 0
		case 9:
			completed := ""
			if registry != nil {
				completed = registry.Complete("/" + strings.TrimPrefix(string(buffer), "/"))
			}
			if completed != "" {
				buffer = []rune(strings.TrimPrefix(completed, "/"))
			}
		case 127, 8:
			if len(buffer) > 0 {
				buffer = buffer[:len(buffer)-1]
			}
		case 27:
			action, err := readTTYPromptEscape(p.reader)
			if err != nil {
				return "", err
			}
			switch action {
			case promptEscapeCancel:
				clearPromptRender(p.out, renderedHints)
				fmt.Fprint(p.out, ttyCRLF)
				return "", ErrPromptInterrupted
			case promptEscapeHistoryUp:
				buffer = []rune(promptHistoryBuffer(p.history, string(buffer), action))
			case promptEscapeHistoryDown:
				buffer = []rune(promptHistoryBuffer(p.history, string(buffer), action))
			}
		default:
			if unicode.IsPrint(r) {
				buffer = append(buffer, r)
			}
		}
		render()
	}
}

type promptEscapeAction int

const (
	promptEscapeCancel promptEscapeAction = iota + 1
	promptEscapeHistoryUp
	promptEscapeHistoryDown
	promptEscapeIgnore
)

func readTTYPromptEscape(reader *bufio.Reader) (promptEscapeAction, error) {
	if reader == nil || reader.Buffered() == 0 {
		return promptEscapeCancel, nil
	}
	next, err := reader.ReadByte()
	if err != nil {
		return promptEscapeIgnore, err
	}
	if next != '[' {
		return promptEscapeCancel, nil
	}
	code, err := reader.ReadByte()
	if err != nil {
		return promptEscapeIgnore, err
	}
	switch code {
	case 'A':
		return promptEscapeHistoryUp, nil
	case 'B':
		return promptEscapeHistoryDown, nil
	default:
		return promptEscapeIgnore, nil
	}
}

func promptHistoryBuffer(history *History, current string, action promptEscapeAction) string {
	if history == nil {
		return current
	}
	switch action {
	case promptEscapeHistoryUp:
		return history.Previous(current)
	case promptEscapeHistoryDown:
		return history.Next()
	default:
		return current
	}
}

func (p *TerminalPrompter) ReadRichLine(prompt string, registry *CommandRegistry) (RichReadResult, error) {
	if !p.tty {
		line, err := p.ReadLine(prompt, registry)
		return RichReadResult{Text: line}, err
	}

	completion := p.completion
	if completion == nil {
		completion = newComposerCompleter(registry)
	}

	editor := richedit.NewEditor(richedit.EditorConfig{
		Prompt:     prompt,
		Out:        p.out,
		In:         p.in,
		History:    p.history,
		Completion: completion,
		Overlay:    p.overlay,
		Chrome:     p.chrome,
	})

	text, images, blocks, err := editor.Run()
	if err != nil {
		if err == richedit.ErrEditorInterrupted {
			return RichReadResult{}, ErrPromptQuit
		}
		if err == richedit.ErrEditorEOF {
			return RichReadResult{}, io.EOF
		}
		return RichReadResult{}, err
	}
	return RichReadResult{Text: text, Images: images, ContentBlocks: blocks}, nil
}

func (p *TerminalPrompter) ReadApproval(prompt string) (rune, error) {
	if !p.tty {
		line, err := p.reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return 0, io.EOF
		}
		return rune(strings.ToLower(line)[0]), nil
	}

	state, err := term.MakeRaw(int(p.in.Fd()))
	if err != nil {
		return 0, err
	}
	defer term.Restore(int(p.in.Fd()), state)

	fmt.Fprint(p.out, prompt)
	for {
		r, _, err := p.reader.ReadRune()
		if err != nil {
			return 0, err
		}
		switch unicode.ToLower(r) {
		case 'y', 'n', 'a', 'v', 'q', 'b':
			fmt.Fprintf(p.out, "%c%s", unicode.ToLower(r), ttyCRLF)
			return unicode.ToLower(r), nil
		case 12:
			return 12, nil
		case 3:
			fmt.Fprint(p.out, ttyCRLF)
			return 3, nil
		case 27:
			fmt.Fprint(p.out, ttyCRLF)
			return 27, nil
		}
	}
}

func (p *TerminalPrompter) ReadSecret(prompt string) (string, error) {
	if !p.tty {
		fmt.Fprint(p.out, prompt)
		line, err := p.reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" && errors.Is(err, io.EOF) {
			return "", io.EOF
		}
		return line, nil
	}

	state, err := term.MakeRaw(int(p.in.Fd()))
	if err != nil {
		return "", err
	}
	defer term.Restore(int(p.in.Fd()), state)

	fmt.Fprint(p.out, prompt)
	var buffer []rune
	render := func() {
		fmt.Fprint(p.out, "\r\033[2K", prompt, strings.Repeat("*", len(buffer)))
	}
	for {
		r, _, err := p.reader.ReadRune()
		if err != nil {
			return "", err
		}
		switch r {
		case '\r', '\n':
			fmt.Fprint(p.out, ttyCRLF)
			return string(buffer), nil
		case 3:
			fmt.Fprint(p.out, ttyCRLF)
			return "", ErrPromptInterrupted
		case 27:
			fmt.Fprint(p.out, ttyCRLF)
			return "", ErrPromptInterrupted
		case 12:
			fmt.Fprint(p.out, "\033[2J\033[H")
		case 4:
			if len(buffer) == 0 {
				fmt.Fprint(p.out, ttyCRLF)
				return "", io.EOF
			}
		case 127, 8:
			if len(buffer) > 0 {
				buffer = buffer[:len(buffer)-1]
			}
		default:
			if unicode.IsPrint(r) {
				buffer = append(buffer, r)
			}
		}
		render()
	}
}

func clearPromptRender(out io.Writer, extraLines int) {
	fmt.Fprint(out, "\r\033[2K")
	for range extraLines {
		fmt.Fprint(out, ttyCRLF, "\033[2K")
	}
	if extraLines > 0 {
		fmt.Fprintf(out, "\033[%dA", extraLines)
	}
}
