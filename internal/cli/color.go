package cli

import (
	"io"
	"os"
)

// color.go holds the tiny ANSI colour helpers the CLI uses for emphasis (a red
// error banner, notably). Colour is applied ONLY when the destination is a real
// terminal, so piped/redirected output and CI logs stay plain (no escape codes
// polluting a captured log). The check is per-writer: stderr may be a TTY while
// stdout is piped, or vice versa.

const (
	ansiRed   = "\x1b[31m"
	ansiBold  = "\x1b[1m"
	ansiReset = "\x1b[0m"
)

// isTTY reports whether w is a real terminal. Only *os.File writers can be a
// terminal; anything else (a bytes.Buffer in tests, a pipe) is not, so colour is
// suppressed there. The terminal test is dependency-free: a character device (an
// os.ModeCharDevice in the file's mode) is a TTY, a regular file / pipe is not.
// This keeps test output deterministic (no escape codes) without any test having
// to opt out, and avoids adding a new module dependency just for the check.
//
// NO_COLOR (https://no-color.org) is honoured: any non-empty value forces colour
// off regardless of the TTY, the widely-respected opt-out.
func isTTY(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// redln writes msg to w in bold red WHEN w is a terminal, and plainly otherwise,
// always followed by a newline. It is the one place the error-emphasis styling
// lives, so callers just say redln(stderr, "...") without repeating the TTY guard.
func redln(w io.Writer, msg string) {
	if isTTY(w) {
		io.WriteString(w, ansiBold+ansiRed+msg+ansiReset+"\n")
		return
	}
	io.WriteString(w, msg+"\n")
}
