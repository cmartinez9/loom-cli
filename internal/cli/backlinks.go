package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cmartinez9/loom-cli/internal/loom"
)

// backlinkRow is one row of `backlinks`: a page that links to or embeds the
// video (kind "backlink"), or with --links an in-player CTA link (kind
// "link").
type backlinkRow struct {
	Kind      string  `json:"kind"`
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	URL       string  `json:"url"`
	Source    string  `json:"source,omitempty"`
	IsSynced  *bool   `json:"is_synced,omitempty"`
	Start     float64 `json:"start,omitempty"`
	End       float64 `json:"end,omitempty"`
	Timestamp string  `json:"timestamp,omitempty"`
}

// backlinkData is what the live fetch returns.
type backlinkData struct {
	Backlinks []loom.Backlink
	Links     []loom.VideoLink
}

func init() {
	registerNovelCommand(func(root *cobra.Command, flags *rootFlags) {
		addNovelCommandIfAbsent(root, newBacklinksCmd(flags))
	})
}

func newBacklinksCmd(flags *rootFlags) *cobra.Command {
	var links bool
	var dbPath string
	cmd := &cobra.Command{
		Use:   "backlinks [video]",
		Short: "List the pages that link to or embed a video (Confluence, Jira, ...)",
		Long: `List the pages that link to or embed a video, as Loom tracks them
(Confluence, Jira and other integrations). --links also includes the
call-to-action links shown inside the player, with their time ranges.
Every row has a "kind" of backlink or link. Backlinks are not kept in the
local store, so --data-source local is a usage error (exit 2).`,
		Example: strings.Trim(`
  loom-pp-cli backlinks latest
  loom-pp-cli backlinks 8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60 --links --json
  loom-pp-cli backlinks https://www.loom.com/share/8f3c1a9e2b7d4c6f9a0e1d2c3b4a5f60`, "\n"),
		Annotations: videoReadAnnotations(sourceLive),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return writeDryRun(cmd.OutOrStdout(), flags, "backlinks")
			}
			arg, err := requireVideoArg(cmd, args)
			if err != nil {
				return err
			}
			res, err := runVideoRead(cmd, flags, dbPath, arg, videoRead[backlinkData]{
				what: "backlinks",
				live: func(ctx context.Context, lc *loom.Client, id string) (backlinkData, error) {
					var d backlinkData
					var err error
					if d.Backlinks, err = lc.FetchBacklinks(ctx, id); err != nil {
						return d, err
					}
					if links {
						if d.Links, err = lc.FetchVideoLinks(ctx, id); err != nil {
							return d, err
						}
					}
					return d, nil
				},
				empty: func(string) backlinkData { return backlinkData{} },
			})
			if err != nil {
				return err
			}
			rows := buildBacklinkRows(res.Value)
			return emit(cmd, flags, rows, func(w io.Writer) error {
				if len(rows) == 0 {
					if !res.Missing {
						if links {
							fmt.Fprintln(w, "No backlinks or in-video links for this video.")
						} else {
							fmt.Fprintln(w, "No backlinks for this video.")
						}
					}
					return nil
				}
				for _, r := range rows {
					where := r.Source
					if r.Kind == "link" {
						where = "at " + r.Timestamp
					}
					fmt.Fprintf(w, "%-8s  %s  %s  %s\n", r.Kind, where, r.Title, r.URL)
				}
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&links, "links", false, "Also include the call-to-action links shown inside the player")
	addVideoDBFlag(cmd, &dbPath)
	return cmd
}

// buildBacklinkRows merges backlinks and CTA links into one list.
func buildBacklinkRows(d backlinkData) []backlinkRow {
	rows := make([]backlinkRow, 0, len(d.Backlinks)+len(d.Links))
	for _, b := range d.Backlinks {
		synced := b.IsSynced
		rows = append(rows, backlinkRow{Kind: "backlink", ID: b.ID, Title: b.Title, URL: b.SourceLink, Source: strings.ToLower(b.Source), IsSynced: &synced})
	}
	for _, l := range d.Links {
		rows = append(rows, backlinkRow{Kind: "link", ID: l.ID, Title: l.Title, URL: l.URL, Start: l.Start, End: l.End, Timestamp: l.Timestamp})
	}
	return rows
}
