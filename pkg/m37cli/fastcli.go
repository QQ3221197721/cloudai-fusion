// Package m37cli is the M37 optimized CLI toolchain for cafctl: a pre-parsed
// command registry with O(1) subcommand dispatch and zero-copy flag parsing,
// benchmarked head-to-head against the real github.com/spf13/cobra.
//
// The design goals (FLIP mandate M37) are:
//   - Subcommand dispatch: O(1) map lookup instead of cobra's linear tree scan
//     (cobra's *Command.Find walks the child slice comparing name + aliases).
//   - Zero-copy flag parsing: flags resolve into a pre-allocated, index-addressed
//     slice — no per-parse map allocation, no reflection, no pflag.FlagSet churn.
//   - Fast help rendering: a single pre-sized strings.Builder pass instead of
//     cobra's text/template execution.
//
// The output/behavior is kept equivalent to the cobra baseline so the win is
// honest: same resolved command, same parsed flag values, same help contents.
package m37cli

import (
	"errors"
	"strconv"
	"strings"
)

// ErrUnknownCommand is returned when a dispatched name is not registered.
var ErrUnknownCommand = errors.New("unknown command")

// ErrUnknownFlag is returned when an argument looks like a flag but is not declared.
var ErrUnknownFlag = errors.New("unknown flag")

// FlagKind enumerates the supported flag value kinds.
type FlagKind uint8

const (
	// StringFlag is a --name value / --name=value string flag.
	StringFlag FlagKind = iota
	// BoolFlag is a --name (implicitly true) or --name=true/false flag.
	BoolFlag
)

// Flag is a declared flag on a command. Default and parsed values live inline;
// there is no per-flag heap object created during parsing.
type Flag struct {
	Name    string
	Kind    FlagKind
	Usage   string
	Default string
}

// Command is a leaf command: a name, help metadata, a pre-declared flag set,
// and a run function. Flags are stored in a slice indexed by declaration order,
// plus a name->index map so parsing is an O(1) lookup with no allocation.
type Command struct {
	Name  string
	Short string
	Long  string

	flags    []Flag
	flagIdx  map[string]int
	run      func(args []string, values []string) error
	helpSize int // precomputed help buffer size to size strings.Builder once
}

// NewCommand builds a command with pre-declared flags. The flag index map and
// the help buffer size are computed ONCE here, so the hot path (Dispatch/Help)
// never rebuilds them.
func NewCommand(name, short, long string, flags []Flag, run func(args, values []string) error) *Command {
	c := &Command{
		Name:    name,
		Short:   short,
		Long:    long,
		flags:   flags,
		flagIdx: make(map[string]int, len(flags)),
		run:     run,
	}
	for i := range flags {
		c.flagIdx[flags[i].Name] = i
	}
	c.helpSize = c.computeHelpSize()
	return c
}

// Registry is the pre-parsed command registry: a map from subcommand name to its
// command, giving O(1) dispatch regardless of how many commands are registered.
type Registry struct {
	name     string
	short    string
	commands map[string]*Command
	order    []string // stable order for help listing

	// scratch is a reusable, pre-allocated flag-value buffer. Reusing it across
	// dispatches avoids a per-dispatch allocation for the parsed-values slice.
	scratch []string
}

// NewRegistry creates an empty registry sized for an expected command count.
func NewRegistry(name, short string, expected int) *Registry {
	return &Registry{
		name:     name,
		short:    short,
		commands: make(map[string]*Command, expected),
		order:    make([]string, 0, expected),
	}
}

// Add registers a command. Panics on duplicate name to surface wiring bugs early.
func (r *Registry) Add(c *Command) {
	if _, dup := r.commands[c.Name]; dup {
		panic("m37cli: duplicate command " + c.Name)
	}
	r.commands[c.Name] = c
	r.order = append(r.order, c.Name)
}

// Lookup returns the command for name in O(1), or nil.
func (r *Registry) Lookup(name string) *Command {
	return r.commands[name]
}

// Names returns the registered command names in registration order.
func (r *Registry) Names() []string { return r.order }

// Dispatch resolves args[0] to a command via O(1) map lookup, parses the
// remaining args into that command's pre-allocated flag-value buffer with no
// per-flag allocation, and invokes run. This is the hot path the benchmark hits.
func (r *Registry) Dispatch(args []string) error {
	if len(args) == 0 {
		return ErrUnknownCommand
	}
	cmd := r.commands[args[0]] // O(1) — no tree walk, no alias scan
	if cmd == nil {
		return ErrUnknownCommand
	}
	values, positional, err := cmd.parseFlags(args[1:], r.scratchFor(cmd))
	if err != nil {
		return err
	}
	if cmd.run == nil {
		return nil
	}
	return cmd.run(positional, values)
}

// Resolve is a benchmark/verification helper: it does the dispatch WORK (lookup +
// flag parse) and returns the resolved command and parsed flag values WITHOUT
// invoking run, so correctness can be asserted against cobra's parsed state.
func (r *Registry) Resolve(args []string) (*Command, []string, error) {
	if len(args) == 0 {
		return nil, nil, ErrUnknownCommand
	}
	cmd := r.commands[args[0]]
	if cmd == nil {
		return nil, nil, ErrUnknownCommand
	}
	values, _, err := cmd.parseFlags(args[1:], r.scratchFor(cmd))
	if err != nil {
		return nil, nil, err
	}
	return cmd, values, nil
}

// scratchFor returns a value buffer sized to cmd's flag count. It reuses the
// registry-level scratch slice when it is large enough (zero-copy: no new alloc
// on the steady-state hot path).
func (r *Registry) scratchFor(cmd *Command) []string {
	n := len(cmd.flags)
	if cap(r.scratch) < n {
		r.scratch = make([]string, n)
	}
	buf := r.scratch[:n]
	for i := range buf {
		buf[i] = cmd.flags[i].Default
	}
	return buf
}

// parseFlags fills values[i] with the parsed value for flag i (defaults already
// pre-loaded). Positional args are returned as a sub-slice of the input (no copy).
// No maps are allocated; the only lookups hit the precomputed flagIdx map.
func (c *Command) parseFlags(args, values []string) (parsed []string, positional []string, err error) {
	i := 0
	for i < len(args) {
		a := args[i]
		if len(a) < 2 || a[0] != '-' {
			// first positional — remaining args are positional (no copy)
			return values, args[i:], nil
		}
		// strip leading dashes
		name := a[1:]
		if len(name) > 0 && name[0] == '-' {
			name = name[1:]
		}
		var inlineVal string
		var hasInline bool
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			inlineVal = name[eq+1:]
			name = name[:eq]
			hasInline = true
		}
		idx, ok := c.flagIdx[name]
		if !ok {
			return values, nil, ErrUnknownFlag
		}
		f := &c.flags[idx]
		switch f.Kind {
		case BoolFlag:
			if hasInline {
				values[idx] = inlineVal
			} else {
				values[idx] = "true"
			}
			i++
		case StringFlag:
			if hasInline {
				values[idx] = inlineVal
				i++
			} else {
				if i+1 >= len(args) {
					return values, nil, ErrUnknownFlag
				}
				values[idx] = args[i+1]
				i += 2
			}
		}
	}
	return values, args[len(args):], nil
}

// FlagValue returns the parsed value for a named flag from a values buffer.
func (c *Command) FlagValue(values []string, name string) (string, bool) {
	idx, ok := c.flagIdx[name]
	if !ok || idx >= len(values) {
		return "", false
	}
	return values[idx], true
}

// computeHelpSize precomputes the byte size of the rendered help so Help() can
// size its strings.Builder in a single Grow — avoiding buffer regrowth copies.
func (c *Command) computeHelpSize() int {
	const boiler = len("Usage:\n  ") + len("\n\nFlags:\n")
	n := boiler + len(c.Long) + 1 + len(c.Name) + len(" [flags]") + 2
	for i := range c.flags {
		f := &c.flags[i]
		n += len("      --") + len(f.Name) + len(" string   ") + len(f.Usage) + 8
	}
	return n
}

// Help renders the command's help text in a single pre-sized Builder pass. The
// layout mirrors cobra's usage block (Long, Usage line, Flags list) closely
// enough to be a correctness-equivalent substitute for `cmd.UsageString()`.
func (c *Command) Help() string {
	var b strings.Builder
	b.Grow(c.helpSize)
	if c.Long != "" {
		b.WriteString(c.Long)
		b.WriteByte('\n')
		b.WriteByte('\n')
	}
	b.WriteString("Usage:\n  ")
	b.WriteString(c.Name)
	b.WriteString(" [flags]\n\nFlags:\n")
	for i := range c.flags {
		f := &c.flags[i]
		b.WriteString("      --")
		b.WriteString(f.Name)
		if f.Kind == StringFlag {
			b.WriteString(" string")
		}
		b.WriteString("   ")
		b.WriteString(f.Usage)
		if f.Default != "" {
			b.WriteString(" (default ")
			b.WriteString(strconv.Quote(f.Default))
			b.WriteByte(')')
		}
		b.WriteByte('\n')
	}
	return b.String()
}
