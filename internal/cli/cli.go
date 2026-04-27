// Package cli implements the claudewatch / cw subcommand interface.
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/cfstout/claudewatch/internal/client"
)

// Run dispatches a subcommand. argv is os.Args[1:] (the subcommand and its
// args). baseURL is the daemon URL. defaultSnoozeMinutes is used for
// `snooze` without --minutes. stdout/stderr are injected for tests.
func Run(argv []string, baseURL string, defaultSnoozeMinutes int, stdout, stderr io.Writer) int {
	c := client.New(baseURL)
	sub := "pending"
	args := []string{}
	if len(argv) > 0 {
		sub = argv[0]
		args = argv[1:]
	}
	switch sub {
	case "pending":
		return cmdPending(c, args, stdout, stderr)
	case "next":
		return cmdNext(c, stdout, stderr)
	case "clear":
		return cmdClear(c, args, stderr)
	case "snooze":
		return cmdSnooze(c, args, defaultSnoozeMinutes, stderr)
	case "summary":
		return cmdSummary(c, args, stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown subcommand: %s\n", sub)
		printUsage(stderr)
		return 2
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `usage: claudewatch [subcommand] [args]
       cw [subcommand] [args]

Subcommands:
  (default)             list pending sessions (alias for `+"`pending`"+`)
  daemon [--port N]     run the daemon in the foreground
  pending [--project P] list pending sessions
  next                  print the oldest pending session name (exit 1 if none)
  clear <name>          mark a session idle
  snooze <name> [--minutes N]
                        snooze a session (default minutes from config)
  summary [--format tmux|json]
                        aggregate counts (default: human, tmux for status line)
`)
}

func cmdPending(c *client.Client, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pending", flag.ContinueOnError)
	fs.SetOutput(stderr)
	project := fs.String("project", "", "filter by project")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	sessions, err := c.List("pending", *project)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	PrintSessions(stdout, sessions)
	return 0
}

func cmdNext(c *client.Client, stdout, stderr io.Writer) int {
	name, err := c.Next()
	if err != nil {
		var nf client.ErrNotFound
		if errors.As(err, &nf) {
			return 1 // nothing pending — exit 1, no output
		}
		fmt.Fprintln(stderr, err)
		return 2
	}
	fmt.Fprintln(stdout, name)
	return 0
}

func cmdClear(c *client.Client, args []string, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: cw clear <name>")
		return 2
	}
	if err := c.Clear(args[0]); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func cmdSnooze(c *client.Client, args []string, defaultMinutes int, stderr io.Writer) int {
	fs := flag.NewFlagSet("snooze", flag.ContinueOnError)
	fs.SetOutput(stderr)
	minutes := fs.Int("minutes", 0, "snooze duration in minutes (0 = daemon default)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(stderr, "usage: cw snooze <name> [--minutes N]")
		return 2
	}
	m := *minutes
	if m == 0 {
		m = defaultMinutes
	}
	if err := c.Snooze(fs.Arg(0), m); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func cmdSummary(c *client.Client, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("summary", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "human", "human | tmux | json")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	sum, err := c.Summary()
	if err != nil {
		// For tmux/json callers, swallow the error and emit empty output —
		// the status line shouldn't render stack traces.
		switch *format {
		case "tmux":
			return 0
		case "json":
			fmt.Fprintln(stdout, "{}")
			return 0
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	switch *format {
	case "tmux":
		fmt.Fprint(stdout, FormatSummaryTmux(sum))
	case "json":
		_ = json.NewEncoder(stdout).Encode(sum)
	default:
		fmt.Fprintf(stdout, "pending: %d  oldest: %s\n",
			sum.TotalPending,
			renderAge(time.Duration(sum.OldestAgeSeconds)*time.Second))
		for proj, n := range sum.ByProject {
			fmt.Fprintf(stdout, "  %-20s %d\n", proj, n)
		}
	}
	return 0
}
