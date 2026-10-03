package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/Blathe/rezgen/internal/store"
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
	st := store.New(*root)
	apps, err := st.List()
	if err != nil {
		fmt.Fprintln(stderr, err)
		if len(apps) == 0 {
			return 1
		}
	}
	if legacy, _ := st.FindLegacy(); len(legacy) > 0 {
		fmt.Fprintf(stderr, "%s from an older version aren't listed; open rezgen to convert them.\n\n", countNoun(len(legacy), "application"))
	}
	if len(apps) == 0 {
		fmt.Fprintf(stdout, "No applications in %s yet.\n", *root)
		return 0
	}
	generated := 0
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "STATUS\tCREATED\tNAME\tDOCUMENTS")
	for _, a := range apps {
		docs := "-"
		if a.Status() == store.Generated {
			generated++
			docs = st.DocsDir(a)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", a.Status(), store.Age(a.Created, now()), clip(a.Name, 50), docs)
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
