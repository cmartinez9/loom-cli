package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cmartinez9/loom-cli/internal/client"
	"github.com/cmartinez9/loom-cli/internal/loom"
)

// fetchLoomSyncPage runs the Loom operation behind one sync resource and
// reshapes its page into the {<field>: {nodes, pageInfo}} connection the
// generated GraphQL sync loop consumes. Nodes are normalized records (see
// loom.Video) so the typed store columns populate.
//
// Loom's library is newest-first, so --since trims older videos and ends the
// walk at the first page that crosses the cutoff.
func fetchLoomSyncPage(ctx context.Context, c *client.Client, resource, fieldPath, sinceTS string, variables map[string]any) (json.RawMessage, error) {
	lc := loom.New(c)
	first, _ := variables["first"].(int)
	if first <= 0 {
		first = 50
	}
	after, _ := variables["after"].(string)

	type pageInfo struct {
		HasNextPage bool   `json:"hasNextPage"`
		EndCursor   string `json:"endCursor"`
	}
	type conn struct {
		Nodes    []any    `json:"nodes"`
		PageInfo pageInfo `json:"pageInfo"`
	}
	out := conn{Nodes: make([]any, 0)}

	switch resource {
	case "videos":
		page, err := lc.ListVideos(ctx, loom.ListOptions{Limit: first, Cursor: after})
		if err != nil {
			return nil, err
		}
		out.PageInfo = pageInfo{HasNextPage: page.HasNextPage, EndCursor: page.EndCursor}
		for _, v := range page.Videos {
			if sinceTS != "" && v.CreatedAt != "" && v.CreatedAt < sinceTS {
				out.PageInfo.HasNextPage = false
				continue
			}
			out.Nodes = append(out.Nodes, v)
		}
	case "folders":
		// Fetchers normalize a signed-out session and return an empty
		// page under --dry-run.
		folders, info, err := lc.PublishedFolders(ctx, first, after)
		if err != nil {
			return nil, err
		}
		if after == "" && !lc.IsDryRun() {
			if mine, err := lc.MyVideosFolder(ctx); err == nil && mine != nil {
				out.Nodes = append(out.Nodes, mine)
			}
		}
		for _, f := range folders {
			out.Nodes = append(out.Nodes, f)
		}
		out.PageInfo = pageInfo{HasNextPage: info.HasNextPage, EndCursor: info.EndCursor}
	case "spaces":
		spaces, info, err := lc.SpaceMembershipsPage(ctx, first, after)
		if err != nil {
			return nil, err
		}
		for _, sp := range spaces {
			out.Nodes = append(out.Nodes, sp.Space)
		}
		out.PageInfo = pageInfo{HasNextPage: info.HasNextPage, EndCursor: info.EndCursor}
	default:
		return nil, fmt.Errorf("unknown resource %q", resource)
	}
	return json.Marshal(map[string]any{fieldPath: out})
}
