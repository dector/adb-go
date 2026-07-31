package custom

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

func writeJSON(stdout io.Writer, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "%s\n", body)
	return err
}

type tableRow []string

func writeAlignedTable(stdout io.Writer, headers []string, rows []tableRow) error {
	if len(headers) == 0 {
		return nil
	}
	widths := tableWidths(headers, rows)
	if err := writeAlignedTableRow(stdout, headers, widths); err != nil {
		return err
	}
	for _, row := range rows {
		if err := writeAlignedTableRow(stdout, tableCells(row, len(headers)), widths); err != nil {
			return err
		}
	}
	return nil
}

func writePlainTable(stdout io.Writer, headers []string, rows []tableRow) error {
	if len(headers) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(stdout, strings.Join(headers, "\t")); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintln(stdout, strings.Join(tableCells(row, len(headers)), "\t")); err != nil {
			return err
		}
	}
	return nil
}

func tableWidths(headers []string, rows []tableRow) []int {
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = utf8.RuneCountInString(header)
	}
	for _, row := range rows {
		for i, cell := range tableCells(row, len(headers)) {
			if n := utf8.RuneCountInString(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}
	return widths
}

func writeAlignedTableRow(stdout io.Writer, cells []string, widths []int) error {
	for i, cell := range cells {
		if i > 0 {
			if _, err := fmt.Fprint(stdout, "  "); err != nil {
				return err
			}
		}
		if i == len(cells)-1 {
			if _, err := fmt.Fprint(stdout, cell); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(stdout, "%-*s", widths[i], cell); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(stdout)
	return err
}

func tableCells(row tableRow, width int) []string {
	cells := make([]string, width)
	for i := range cells {
		if i < len(row) {
			cells[i] = row[i]
		}
	}
	return cells
}

type cliOptions struct {
	JSON  bool
	Quiet bool
}

type outputPolicy struct {
	quiet bool
	info  io.Writer
}

func newOutputPolicy(stdout io.Writer, quiet bool) outputPolicy {
	return outputPolicy{quiet: quiet, info: stdout}
}

func (p outputPolicy) Infof(format string, args ...any) {
	if p.quiet {
		return
	}
	fmt.Fprintf(p.info, format, args...)
}

func (p outputPolicy) Infoln(args ...any) {
	if p.quiet {
		return
	}
	fmt.Fprintln(p.info, args...)
}
