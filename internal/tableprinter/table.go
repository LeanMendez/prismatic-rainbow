// Package tableprinter renders rows as an aligned table on a TTY or as
// header-less TSV when piped, like gh's tableprinter.
package tableprinter

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

type TablePrinter struct {
	out     io.Writer
	isTTY   bool
	headers []string
	rows    [][]string
}

func New(out io.Writer, isTTY bool) *TablePrinter {
	return &TablePrinter{out: out, isTTY: isTTY}
}

func (t *TablePrinter) AddHeader(h ...string) { t.headers = h }
func (t *TablePrinter) AddRow(f ...string)    { t.rows = append(t.rows, f) }

func (t *TablePrinter) Render() error {
	if !t.isTTY {
		for _, r := range t.rows {
			if _, err := fmt.Fprintln(t.out, strings.Join(r, "\t")); err != nil {
				return err
			}
		}
		return nil
	}
	all := t.rows
	if len(t.headers) > 0 {
		all = append([][]string{t.headers}, t.rows...)
	}
	widths := map[int]int{}
	for _, r := range all {
		for i, f := range r {
			if n := utf8.RuneCountInString(f); n > widths[i] {
				widths[i] = n
			}
		}
	}
	for _, r := range all {
		var b strings.Builder
		for i, f := range r {
			b.WriteString(f)
			if i < len(r)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-utf8.RuneCountInString(f)+2))
			}
		}
		if _, err := fmt.Fprintln(t.out, b.String()); err != nil {
			return err
		}
	}
	return nil
}
