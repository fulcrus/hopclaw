package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

type targetPrompter struct {
	in     io.Reader
	out    io.Writer
	reader *bufio.Reader
	file   *os.File
	tty    bool
}

func newTargetPrompter(in io.Reader, out io.Writer) *targetPrompter {
	file, tty := readerFile(in)
	return &targetPrompter{
		in:     in,
		out:    out,
		reader: bufio.NewReader(in),
		file:   file,
		tty:    tty,
	}
}

func (p *targetPrompter) CanPrompt() bool {
	return p == nil || p.tty || p.file == nil
}

func (p *targetPrompter) PromptText(label, initial string, required bool) (string, error) {
	for {
		value, err := p.readLine(label, initial)
		if err != nil {
			return "", err
		}
		value = strings.TrimSpace(value)
		if value != "" || !required {
			return value, nil
		}
		fmt.Fprintf(p.out, "%s is required.\n", label)
	}
}

func (p *targetPrompter) PromptChoice(label, initial string, allowed []string) (string, error) {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, item := range allowed {
		allowedSet[strings.ToLower(strings.TrimSpace(item))] = struct{}{}
	}
	for {
		value, err := p.readLine(label, initial)
		if err != nil {
			return "", err
		}
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			value = strings.ToLower(strings.TrimSpace(initial))
		}
		if _, ok := allowedSet[value]; ok {
			return value, nil
		}
		fmt.Fprintf(p.out, "Please choose one of: %s\n", strings.Join(allowed, ", "))
	}
}

func (p *targetPrompter) PromptSecret(label string, required bool) (string, error) {
	for {
		value, err := p.readSecret(label)
		if err != nil {
			return "", err
		}
		value = strings.TrimSpace(value)
		if value != "" || !required {
			return value, nil
		}
		fmt.Fprintf(p.out, "%s is required.\n", label)
	}
}

func (p *targetPrompter) readLine(label, initial string) (string, error) {
	prompt := label
	if initial = strings.TrimSpace(initial); initial != "" {
		prompt += " [" + initial + "]"
	}
	fmt.Fprintf(p.out, "%s: ", prompt)
	line, err := p.reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(line) == "" && initial != "" {
		return initial, nil
	}
	if errors.Is(err, io.EOF) && strings.TrimSpace(line) == "" {
		if initial != "" {
			return initial, nil
		}
		return "", io.EOF
	}
	return line, nil
}

func (p *targetPrompter) readSecret(label string) (string, error) {
	fmt.Fprintf(p.out, "%s: ", label)
	if p.tty && p.file != nil {
		value, err := term.ReadPassword(int(p.file.Fd()))
		fmt.Fprintln(p.out)
		if err != nil {
			return "", err
		}
		return string(value), nil
	}
	line, err := p.reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if errors.Is(err, io.EOF) && strings.TrimSpace(line) == "" {
		return "", io.EOF
	}
	return line, nil
}
