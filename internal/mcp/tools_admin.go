package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	pb "github.com/ttpreport/ligolo-mp/v2/protobuf"
)

// registerAdminReadTools registers admin-only read tools. It is called only
// when the connected operator is an admin (the server enforces this regardless,
// but registering conditionally keeps the tool list honest for the host). These
// are read-only: operator/cert metadata only. Admin *write* tools (add/del/
// promote/demote operator, regen cert, generate agent) are milestone M3 and are
// gated behind --allow-admin.
func registerAdminReadTools(s *mcp.Server, c *Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "ligolo_list_operators",
		Description: "List all operators registered on the ligolo-mp server, with their admin flag and online status. Admin-only.",
		Annotations: readOnly("List operators"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, operatorsOutput, error) {
		resp, err := c.Ligolo().GetOperators(ctx, &pb.Empty{})
		if err != nil {
			return nil, operatorsOutput{}, err
		}
		return nil, toOperatorsOutput(resp), nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "ligolo_list_certs",
		Description: "List certificate metadata (name and expiry) known to the ligolo-mp server. Never returns certificate or private-key material. Admin-only.",
		Annotations: readOnly("List certificates"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, certsOutput, error) {
		resp, err := c.Ligolo().GetCerts(ctx, &pb.Empty{})
		if err != nil {
			return nil, certsOutput{}, err
		}
		return nil, toCertsOutput(resp), nil
	})
}
