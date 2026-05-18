//go:build darwin || linux || freebsd || netbsd || openbsd || dragonfly

package repl

import (
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

const (
	runKeyPollInterval = 100 * time.Millisecond
	escSequenceWait    = 30 * time.Millisecond
)

func startRunKeyListener(input *os.File) (<-chan rune, func(), error) {
	if input == nil {
		return nil, func() {}, nil
	}
	fd := int(input.Fd())
	if !term.IsTerminal(fd) {
		return nil, func() {}, nil
	}
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, nil, err
	}

	out := make(chan rune, 1)
	done := make(chan struct{})
	var once sync.Once
	stop := func() {
		once.Do(func() {
			close(done)
			_ = term.Restore(fd, state)
		})
	}

	go func() {
		defer close(out)
		defer stop()

		var buf [1]byte
		for {
			ready, err := pollReadable(fd, runKeyPollInterval)
			if err != nil {
				return
			}
			select {
			case <-done:
				return
			default:
			}
			if !ready {
				continue
			}
			n, err := input.Read(buf[:])
			if err != nil || n == 0 {
				return
			}
			key := rune(buf[0])
			switch key {
			case escKey:
				drained, err := drainEscapeSequence(fd, input)
				if err != nil {
					return
				}
				if drained {
					continue
				}
			case 3, 12, 'q', 'Q', 'b', 'B':
			default:
				continue
			}
			select {
			case out <- key:
			case <-done:
				return
			}
		}
	}()

	return out, stop, nil
}

func pollReadable(fd int, timeout time.Duration) (bool, error) {
	millis := int(timeout / time.Millisecond)
	if millis <= 0 {
		millis = 1
	}
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	for {
		n, err := unix.Poll(fds, millis)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return false, err
		}
		return n > 0 && fds[0].Revents&unix.POLLIN != 0, nil
	}
}

func drainEscapeSequence(fd int, input *os.File) (bool, error) {
	drained := false
	var buf [1]byte
	for {
		ready, err := pollReadable(fd, escSequenceWait)
		if err != nil {
			return drained, err
		}
		if !ready {
			return drained, nil
		}
		n, err := input.Read(buf[:])
		if err != nil {
			return drained, err
		}
		if n == 0 {
			return drained, nil
		}
		drained = true
	}
}
