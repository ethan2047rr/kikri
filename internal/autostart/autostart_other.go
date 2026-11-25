//go:build !darwin && !windows

package autostart

import "errors"

var errUnsupported = errors.New("autostart is not supported on this platform")

func Enabled() (bool, error) { return false, nil }
func Set(bool) error         { return errUnsupported }
