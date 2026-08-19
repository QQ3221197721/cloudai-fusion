// Package reporting - export/serialization of reports to JSON and CSV.
package reporting

import (
	"bufio"
	"encoding/json"
	"io"
	"sort"
	"strconv"
)

// WriteJSON serializes a report as indented JSON. It uses the standard library
// encoder directly against the writer so large reports stream out rather than
// being buffered into an intermediate byte slice.
func WriteJSON(w io.Writer, report *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

// WriteJSONCompact serializes a report as compact single-line JSON (no
// indentation), the form used for machine-to-machine export where size matters.
func WriteJSONCompact(w io.Writer, report *Report) error {
	return json.NewEncoder(w).Encode(report)
}

// WriteCSV serializes report rows as CSV. The header is the sorted dimension
// names followed by the fixed metric columns. Row values are written in the
// same dimension order so the output is stable and diff-friendly.
//
// A hand-rolled writer (rather than encoding/csv) is used because report cells
// never contain commas, quotes, or newlines — the dimension values are
// namespace/tenant/resource/region identifiers — so we can skip per-field
// quoting analysis and the associated allocations on the export hot path.
func WriteCSV(w io.Writer, report *Report) error {
	bw := bufio.NewWriter(w)

	dims := append([]string(nil), report.Dimensions...)
	sort.Strings(dims)

	// Header.
	for _, d := range dims {
		if _, err := bw.WriteString(d); err != nil {
			return err
		}
		if err := bw.WriteByte(','); err != nil {
			return err
		}
	}
	if _, err := bw.WriteString("count,quantity,cost\n"); err != nil {
		return err
	}

	// Rows.
	for i := range report.Rows {
		row := &report.Rows[i]
		for _, d := range dims {
			if _, err := bw.WriteString(row.Keys[d]); err != nil {
				return err
			}
			if err := bw.WriteByte(','); err != nil {
				return err
			}
		}
		if err := writeInt(bw, row.Count); err != nil {
			return err
		}
		if err := bw.WriteByte(','); err != nil {
			return err
		}
		if err := writeInt(bw, row.Quantity); err != nil {
			return err
		}
		if err := bw.WriteByte(','); err != nil {
			return err
		}
		if _, err := bw.WriteString(strconv.FormatFloat(row.Cost, 'f', 4, 64)); err != nil {
			return err
		}
		if err := bw.WriteByte('\n'); err != nil {
			return err
		}
	}

	return bw.Flush()
}

// writeInt appends a base-10 integer to the writer using a stack buffer to
// avoid heap allocation from strconv.Itoa on the export hot path.
func writeInt(bw *bufio.Writer, v int64) error {
	var buf [20]byte
	b := strconv.AppendInt(buf[:0], v, 10)
	_, err := bw.Write(b)
	return err
}
