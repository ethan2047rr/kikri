// Package autostart starts Kikri at login ("开机自启"). The system itself is
// the source of truth: a LaunchAgent on macOS, an HKCU Run value on Windows.
package autostart

import "os"

// Label identifies Kikri's login item (also the LaunchAgent file name).
const Label = "io.github.ethan2047rr.kikri"

// executable is the program to start at login; a variable for tests.
var executable = os.Executable
