//go:build !darwin && !linux && !freebsd && !netbsd && !openbsd && !dragonfly

package repl

import "os"

func startRunKeyListener(_ *os.File) (<-chan rune, func(), error) {
	return nil, func() {}, nil
}
