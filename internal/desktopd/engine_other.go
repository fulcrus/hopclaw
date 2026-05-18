//go:build !darwin && !windows && !linux

package desktopd

import "fmt"

func NewDefaultEngine() (Engine, error) {
	return nil, fmt.Errorf("desktop automation is only implemented on darwin in this release")
}
