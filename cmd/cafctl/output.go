// Package main - Output formatting utilities for cafctl CLI
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/fatih/color"
)

// Colors for terminal output
var (
	green         = color.New(color.FgGreen)
	greenBold     = color.New(color.FgGreen, color.Bold)
	red           = color.New(color.FgRed)
	redBold       = color.New(color.FgRed, color.Bold)
	yellow        = color.New(color.FgYellow)
	yellowBold    = color.New(color.FgYellow, color.Bold)
	blue          = color.New(color.FgBlue)
	blueBold      = color.New(color.FgBlue, color.Bold)
	cyan          = color.New(color.FgCyan)
	cyanBold      = color.New(color.FgCyan, color.Bold)
	defaultColor  = color.New()
	successSymbol = "✓"
	errorSymbol   = "✗"
	warningSymbol = "⚠"
	infoSymbol    = "ℹ"
)

// OK returns a formatted success symbol
func OK() string { return successSymbol + " " }

// ERROR returns a formatted error symbol
func ERROR() string { return errorSymbol + " " }

// WARN returns a formatted warning symbol
func WARN() string { return warningSymbol + " " }

// INFO returns a formatted info symbol
func INFO() string { return infoSymbol + " " }

// FormatError safely formats an error or returns default message if nil
func FormatError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// ToJSON marshals v to indented JSON or exits on error
func ToJSON(v interface{}) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		os.Exit(1)
	}
	return string(b)
}

// ToJSONPretty formats JSON with extra spacing
func ToJSONPretty(v interface{}) string {
	b, err := json.MarshalIndent(v, "", "    ")
	if err != nil {
		return "{}"
	}
	return string(b)
}

// PrintSuccess prints a success message
func PrintSuccess(format string, args ...interface{}) {
	greenBold.Printf(format+"\n", args...)
}

// PrintError prints an error message
func PrintError(format string, args ...interface{}) {
	redBold.Fprintf(os.Stderr, format+"\n", args...)
}

// PrintWarning prints a warning message
func PrintWarning(format string, args ...interface{}) {
	yellow.Fprintf(os.Stderr, format+"\n", args...)
}

// PrintInfo prints an info message
func PrintInfo(format string, args ...interface{}) {
	if len(args) == 0 {
		blue.Println(format)
		return
	}
	blue.Printf(format+"\n", args...)
}

// PrintTable outputs a tabular view of key-value pairs
func PrintTable(headers []string, rows [][]string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for i, h := range headers {
		if i > 0 {
			fmt.Fprint(w, "  ")
		}
		cyanBold.Fprintf(w, "%s", h)
	}
	fmt.Fprintln(w)
	for _, row := range rows {
		for j, cell := range row {
			if j > 0 {
				fmt.Fprint(w, "  ")
			}
			fmt.Fprint(w, cell)
		}
		fmt.Fprintln(w)
	}
	w.Flush()
}

// Separator returns a horizontal line separator
func Separator(char rune, length int) string {
	s := ""
	for i := 0; i < length; i++ {
		s += string(char)
	}
	return s
}

// PrintStep writes a staged progress line like "[2/4] Applying manifest…" to w.
// It gives long-running commands (deploy) visible, incremental feedback instead
// of long silence. Uses cyan for the counter so it stands out but stays readable
// with color disabled (the [n/total] text carries the meaning either way).
func PrintStep(w io.Writer, current, total int, msg string) {
	cyanBold.Fprintf(w, "[%d/%d] ", current, total)
	fmt.Fprintln(w, msg)
}

// PrintStepDone writes a completed-step marker under the current step.
func PrintStepDone(w io.Writer, msg string) {
	green.Fprintf(w, "      %s%s\n", successSymbol+" ", msg)
}

// PrintNextSteps renders an actionable "what to do next" block. Every failing
// path in cafctl should end with one of these so the user is never stuck with a
// bare technical error.
func PrintNextSteps(w io.Writer, title string, steps ...string) {
	yellowBold.Fprintf(w, "%s\n", title)
	for _, s := range steps {
		yellow.Fprintf(w, "  • %s\n", s)
	}
}
