package clidocs

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/hurtener/dockyard/internal/cli"
)

// Render writes the dockyard CLI command tree as a Markdown page to w.
//
// The page leads with the root command's Long, then renders each
// subcommand in alphabetical order with its Use, Short, Long and flag
// table. The output is deterministic — given the same command tree,
// Render produces byte-identical bytes — so `make docs` only updates the
// page when the CLI surface actually changed.
func Render(w io.Writer) error {
	root := cli.NewRootCmd(bytes.NewBuffer(nil), bytes.NewBuffer(nil))
	root.Use = "dockyard"

	var out bytes.Buffer
	out.WriteString("# CLI reference\n\n")
	out.WriteString("> Auto-generated from the cobra command tree by `internal/clidocs`. ")
	out.WriteString("Do not hand-edit — re-run `make docs`.\n\n")

	out.WriteString("## `dockyard`\n\n")
	out.WriteString("> ")
	out.WriteString(strings.TrimSpace(root.Short))
	out.WriteString("\n\n")
	if root.Long != "" {
		out.WriteString("```text\n")
		out.WriteString(strings.TrimSpace(root.Long))
		out.WriteString("\n```\n\n")
	}

	cmds := root.Commands()
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name() < cmds[j].Name() })
	for _, c := range cmds {
		// Skip cobra's built-in help/completion commands — they add noise
		// and have no Dockyard-specific behaviour.
		if c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		renderCommand(&out, c)
	}

	// Trim trailing whitespace + ensure exactly one terminating newline so
	// markdownlint's MD012 (no-multiple-blanks) is satisfied.
	rendered := strings.TrimRight(out.String(), " \n\t") + "\n"
	_, err := w.Write([]byte(rendered))
	return err
}

// renderCommand writes one subcommand's section. The flag table is
// pre-sorted by long-name so a rerun with no flag change produces the
// same bytes.
func renderCommand(out *bytes.Buffer, c *cobra.Command) {
	fmt.Fprintf(out, "## `dockyard %s`\n\n", c.Name())
	if c.Short != "" {
		// Render Short as a markdown blockquote rather than `_emphasis_`
		// so markdownlint's MD036 (emphasis-as-heading) does not trip.
		// The semantic intent — a one-line subtitle for the section — is
		// preserved.
		fmt.Fprintf(out, "> %s\n\n", c.Short)
	}
	if c.Use != "" && c.Use != c.Name() {
		// Cobra's Use includes positional argument hints; render it
		// verbatim so the page documents the call shape.
		fmt.Fprintf(out, "**Usage:** `dockyard %s`\n\n", c.Use)
	}
	if c.Long != "" {
		out.WriteString("```text\n")
		out.WriteString(strings.TrimSpace(c.Long))
		out.WriteString("\n```\n\n")
	}

	flags := c.Flags()
	if flags.HasFlags() {
		type row struct {
			name string
			usg  string
			def  string
		}
		var rows []row
		flags.VisitAll(func(f *pflag.Flag) {
			// Skip hidden flags — `--dockyard-path` is the prime
			// example; it is the pre-publish replace seam (D-080) and
			// `MarkHidden`-ed from the CLI surface. The docs reflect
			// the public surface only.
			if f.Hidden {
				return
			}
			rows = append(rows, row{
				name: "--" + f.Name,
				usg:  f.Usage,
				def:  f.DefValue,
			})
		})
		sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
		if len(rows) > 0 {
			out.WriteString("| Flag | Description | Default |\n")
			out.WriteString("| --- | --- | --- |\n")
			for _, r := range rows {
				def := r.def
				if def == "" {
					def = "—"
				}
				fmt.Fprintf(out, "| `%s` | %s | `%s` |\n",
					r.name, escapePipe(r.usg), escapePipe(def))
			}
			out.WriteString("\n")
		}
	}
}

// escapePipe escapes a pipe character in cell content (so a Markdown
// table renders correctly) and the angle-bracket characters that
// VitePress's Vue compiler would otherwise treat as an HTML tag start.
// The result still reads naturally — `&lt;name&gt;` renders as `<name>`.
func escapePipe(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
