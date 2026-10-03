package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Blathe/rezgen/internal/track"
)

// list prints every application and its status.
func list(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("out", "applications", "folder that holds the applications")
	only := fs.String("status", "", "show only applications with this status")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var filter track.Status
	if *only != "" {
		st, err := track.ParseStatus(*only)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		filter = st
	}

	apps, err := track.List(*root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if len(apps) == 0 {
		fmt.Fprintf(stdout, "No applications in %s yet. Run rezgen generate to create one.\n", *root)
		return 0
	}

	counts := map[track.Status]int{}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "STATUS\tUPDATED\tCOMPANY\tROLE\tFOLDER")
	shown := 0
	for _, a := range apps {
		counts[a.Status]++
		if filter != "" && a.Status != filter {
			continue
		}
		shown++
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", a.Status, track.Age(a.Updated(), now()), clip(orDash(a.Company), 30), clip(orDash(a.Role), 40), a.Name)
	}
	tw.Flush()
	if shown == 0 {
		fmt.Fprintf(stdout, "(none with status %s)\n", filter)
	}

	var parts []string
	for _, st := range track.Statuses {
		if n := counts[st]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, st))
		}
	}
	fmt.Fprintf(stdout, "\n%s: %s\n", countNoun(len(apps), "application"), strings.Join(parts, ", "))
	return 0
}

// status shows an application's history, or records a new status for it.
//
//	rezgen status northwind                  show the history
//	rezgen status northwind applied          record a change
//	rezgen status northwind interview -note "phone screen Tuesday"
func status(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("out", "applications", "folder that holds the applications")
	note := fs.String("note", "", "note to record with the change")
	date := fs.String("date", "", "date of the change as YYYY-MM-DD (default today)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: rezgen status <application> [new-status] [-note text] [-date YYYY-MM-DD]")
		fmt.Fprintf(stderr, "statuses: %s\n", statusNames())
		fs.PrintDefaults()
	}
	// Flags may come after the positional arguments ("status acme applied
	// -note x"), so parse them out wherever they are.
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) < 1 || len(pos) > 2 {
		fs.Usage()
		return 2
	}

	app, err := track.Find(*root, pos[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	if len(pos) == 2 {
		st, err := track.ParseStatus(pos[1])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		on := now()
		if *date != "" {
			if on, err = time.Parse("2006-01-02", *date); err != nil {
				fmt.Fprintf(stderr, "bad -date %q: want YYYY-MM-DD\n", *date)
				return 2
			}
		}
		app.Set(st, *note, on)
		if err := track.Save(app.Dir, app.Record); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	} else if *note != "" || *date != "" {
		fmt.Fprintln(stderr, "-note and -date need a status; to add a note without changing it, repeat the current status")
		return 2
	}

	fmt.Fprintf(stdout, "%s, %s (%s)\n", orDash(app.Role), orDash(app.Company), app.Name)
	if app.Source != "" {
		fmt.Fprintf(stdout, "Posting: %s\n", app.Source)
	}
	fmt.Fprintf(stdout, "Status:  %s\n\n", app.Status)
	for _, e := range app.History {
		line := fmt.Sprintf("  %s  %s", e.Date, e.Status)
		if e.Note != "" {
			line += "  " + e.Note
		}
		fmt.Fprintln(stdout, line)
	}
	return 0
}

// parseInterspersed parses flags that may appear before, between or after
// positional arguments, and returns the positional ones.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func statusNames() string {
	names := make([]string, len(track.Statuses))
	for i, st := range track.Statuses {
		names[i] = string(st)
	}
	return strings.Join(names, ", ")
}

// clip shortens s to at most n characters for a table column.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-3])) + "..."
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
