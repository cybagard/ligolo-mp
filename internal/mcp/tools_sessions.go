package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	pb "github.com/ttpreport/ligolo-mp/v2/protobuf"
)

// registerSessionTools registers read-only session tools. State-changing
// session/relay tools (rename, kill, start/stop relay) are milestone M2 and are
// intentionally not registered here.
func registerSessionTools(s *mcp.Server, c *Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "ligolo_list_sessions",
		Description: "List all ligolo-mp agent sessions with their hostname, alias, " +
			"connection and relay state, network interfaces (with IPs), active routes " +
			"(CIDR/metric/loopback), and redirectors. This is the primary recon tool for " +
			"understanding the current pivoting topology.",
		Annotations: readOnly("List sessions"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, sessionsOutput, error) {
		resp, err := c.Ligolo().GetSessions(ctx, &pb.Empty{})
		if err != nil {
			return nil, sessionsOutput{}, err
		}
		return nil, toSessionsOutput(resp), nil
	})
}
