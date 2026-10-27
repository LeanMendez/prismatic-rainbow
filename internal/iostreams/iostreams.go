// Package iostreams abstracts stdin/stdout/stderr with TTY and color
// detection so commands never touch os.Stdout directly and tests can use
// in-memory buffers.
package iostreams

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// IOStreams bundles the three standard streams plus terminal metadata.
type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer

	stdinFd     int
	isStdinTTY  bool
	isStdoutTTY bool
	color       bool

	inReader *bufio.Reader
}

// System returns IOStreams bound to the process' real stdio.
func System() *IOStreams {
	s := &IOStreams{
		In:      os.Stdin,
		Out:     os.Stdout,
		ErrOut:  os.Stderr,
		stdinFd: int(os.Stdin.Fd()),
	}
	s.isStdinTTY = term.IsTerminal(s.stdinFd)
	s.isStdoutTTY = term.IsTerminal(int(os.Stdout.Fd()))
	s.color = s.isStdoutTTY && os.Getenv("NO_COLOR") == ""
	return s
}

// Test returns IOStreams backed by buffers, with TTY detection disabled.
func Test() (s *IOStreams, stdin, stdout, stderr *bytes.Buffer) {
	stdin, stdout, stderr = &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}
	s = &IOStreams{In: stdin, Out: stdout, ErrOut: stderr}
	return
}

// SetTTY forces TTY detection (tests only).
func (s *IOStreams) SetTTY(stdin, stdout bool) {
	s.isStdinTTY = stdin
	s.isStdoutTTY = stdout
}

func (s *IOStreams) IsStdinTTY() bool  { return s.isStdinTTY }
func (s *IOStreams) IsStdoutTTY() bool { return s.isStdoutTTY }

// ReadLine reads one line from stdin without the trailing newline.
func (s *IOStreams) ReadLine() (string, error) {
	if s.inReader == nil {
		s.inReader = bufio.NewReader(s.In)
	}
	line, err := s.inReader.ReadString('\n')
	if err == io.EOF && line != "" {
		err = nil
	}
	return strings.TrimRight(line, "\r\n"), err
}

// ReadSecret reads a line without echo when stdin is a terminal; falls
// back to ReadLine for piped input.
func (s *IOStreams) ReadSecret() (string, error) {
	if s.isStdinTTY {
		b, err := term.ReadPassword(s.stdinFd)
		return string(b), err
	}
	return s.ReadLine()
}

func (s *IOStreams) paint(code, text string) string {
	if !s.color {
		return text
	}
	return code + text + "\x1b[0m"
}

func (s *IOStreams) Bold(t string) string   { return s.paint("\x1b[1m", t) }
func (s *IOStreams) Green(t string) string  { return s.paint("\x1b[32m", t) }
func (s *IOStreams) Red(t string) string    { return s.paint("\x1b[31m", t) }
func (s *IOStreams) Yellow(t string) string { return s.paint("\x1b[33m", t) }
func (s *IOStreams) Gray(t string) string   { return s.paint("\x1b[90m", t) }
