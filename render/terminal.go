package render

import (
	"os"

	"golang.org/x/sys/unix"
)

// Terminal handles raw mode and screen control.
type Terminal struct {
	fd       int
	original unix.Termios
}

// NewTerminal creates a terminal controller for the given file.
func NewTerminal(f *os.File) (*Terminal, error) {
	fd := int(f.Fd())
	termios, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return nil, err
	}
	return &Terminal{fd: fd, original: *termios}, nil
}

// EnterRawMode puts the terminal into raw mode for direct character input.
func (t *Terminal) EnterRawMode() error {
	raw := t.original
	raw.Iflag &^= unix.BRKINT | unix.ICRNL | unix.INPCK | unix.ISTRIP | unix.IXON
	raw.Oflag &^= unix.OPOST
	raw.Cflag |= unix.CS8
	raw.Lflag &^= unix.ECHO | unix.ICANON | unix.IEXTEN | unix.ISIG
	raw.Cc[unix.VMIN] = 0
	raw.Cc[unix.VTIME] = 1
	return unix.IoctlSetTermios(t.fd, unix.TIOCSETA, &raw)
}

// RestoreMode restores the original terminal mode.
func (t *Terminal) RestoreMode() error {
	return unix.IoctlSetTermios(t.fd, unix.TIOCSETA, &t.original)
}

const (
	ClearScreen    = "\033[2J"
	ClearLine      = "\033[2K"
	CursorHome     = "\033[H"
	CursorHide     = "\033[?25l"
	CursorShow     = "\033[?25h"
	AltScreenEnter = "\033[?1049h"
	AltScreenExit  = "\033[?1049l"
)

// EnterAltScreen switches to the alternate screen buffer.
func EnterAltScreen(f *os.File) {
	f.WriteString(AltScreenEnter + CursorHide + ClearScreen)
}

// ExitAltScreen returns to the main screen buffer.
func ExitAltScreen(f *os.File) {
	f.WriteString(CursorShow + AltScreenExit)
}
