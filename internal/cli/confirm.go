package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

var errConfirmationRequired = errors.New("confirmation required in non-interactive mode; rerun with --yes")

func confirmDestructiveAction(in io.Reader, out io.Writer, prompt string) (bool, error) {
	if in == nil {
		in = strings.NewReader("")
	}
	if out == nil {
		out = io.Discard
	}
	if prompt != "" {
		if _, err := fmt.Fprint(out, prompt); err != nil {
			return false, err
		}
	}

	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer = strings.TrimSpace(answer)
	if errors.Is(err, io.EOF) && answer == "" {
		if _, tty := readerFile(in); !tty {
			return false, errConfirmationRequired
		}
	}
	switch strings.ToLower(answer) {
	case "y", "yes":
		return true, nil
	default:
		if _, err := fmt.Fprintln(out, "Cancelled."); err != nil {
			return false, err
		}
		return false, nil
	}
}
