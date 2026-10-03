package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/Blathe/rezgen/internal/track"
)

// list prints every application and whether its documents exist yet.
func list(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("out", "", "folder that holds the applications (default: from settings, else ./applications)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	defaults(nil, root, nil)
	apps, err := track.List(*root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if len(apps) == 0 {
		fmt.Fprintf(stdout, "No applications in %s yet.\n", *root)
		return 0
	}
	generated := 0
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "STATUS\tCREATED\tNAME\tFOLDER")
	for _, a := range apps {
		if a.Status == track.Generated {
			generated++
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", a.Status, track.Age(a.Created, now()), clip(a.Name, 50), a.Folder())
	}
	tw.Flush()
	fmt.Fprintf(stdout, "\n%s: %d generated, %d not started\n", countNoun(len(apps), "application"), generated, len(apps)-generated)
	return 0
}

// clip shortens s to at most n characters for a table column.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-3])) + "..."
}
