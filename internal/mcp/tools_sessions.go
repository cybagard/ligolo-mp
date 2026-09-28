package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	pb "github.com/ttpreport/ligolo-mp/v2/protobuf"
)

// sessionIDInput identifies a session for tools that act on one.
type sessionIDInput struct {
	SessionID string `json:"session_id" jsonschema:"the target session ID (from ligolo_list_sessions)"`
}

type renameSessionInput struct {
	SessionID string `json:"session_id" jsonschema:"the target session ID"`
	Alias     string `json:"alias" jsonschema:"the new human-readable alias for the session"`
}

// registerSessionReadTools registers read-only session tools.
func registerSessionReadTools(s *mcp.Server, c *Client) {
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

// registerSessionWriteTools registers state-changing session and relay tools.
// Registered only when --allow-writes is set.
func registerSessionWriteTools(s *mcp.Server, c *Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "ligolo_rename_session",
		Description: "Set the human-readable alias of a session. Additive; does not affect connectivity.",
		Annotations: writeAction("Rename session", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in renameSessionInput) (*mcp.CallToolResult, actionOutput, error) {
		if in.SessionID == "" {
			return nil, actionOutput{}, fmt.Errorf("session_id is required")
		}
		if _, err := c.Ligolo().RenameSession(ctx, &pb.RenameSessionReq{SessionID: in.SessionID, Alias: in.Alias}); err != nil {
			return nil, actionOutput{}, err
		}
		return nil, actionOutput{OK: true, Message: fmt.Sprintf("renamed session %s to %q", in.SessionID, in.Alias)}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "ligolo_kill_session",
		Description: "Terminate a session: disconnect the agent and remove it from the server. Destructive and not reversible.",
		Annotations: writeAction("Kill session", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in sessionIDInput) (*mcp.CallToolResult, actionOutput, error) {
		if in.SessionID == "" {
			return nil, actionOutput{}, fmt.Errorf("session_id is required")
		}
		if _, err := c.Ligolo().KillSession(ctx, &pb.KillSessionReq{SessionID: in.SessionID}); err != nil {
			return nil, actionOutput{}, err
		}
		return nil, actionOutput{OK: true, Message: fmt.Sprintf("killed session %s", in.SessionID)}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "ligolo_start_relay",
		Description: "Start the relay for a session: bring up its TUN interface and begin routing traffic through the agent pivot. State-changing (affects local network routing).",
		Annotations: writeAction("Start relay", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in sessionIDInput) (*mcp.CallToolResult, actionOutput, error) {
		if in.SessionID == "" {
			return nil, actionOutput{}, fmt.Errorf("session_id is required")
		}
		if _, err := c.Ligolo().StartRelay(ctx, &pb.StartRelayReq{SessionID: in.SessionID}); err != nil {
			return nil, actionOutput{}, err
		}
		return nil, actionOutput{OK: true, Message: fmt.Sprintf("started relay for session %s", in.SessionID)}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "ligolo_stop_relay",
		Description: "Stop the relay for a session: tear down its TUN routing. State-changing.",
		Annotations: writeAction("Stop relay", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in sessionIDInput) (*mcp.CallToolResult, actionOutput, error) {
		if in.SessionID == "" {
			return nil, actionOutput{}, fmt.Errorf("session_id is required")
		}
		if _, err := c.Ligolo().StopRelay(ctx, &pb.StopRelayReq{SessionID: in.SessionID}); err != nil {
			return nil, actionOutput{}, err
		}
		return nil, actionOutput{OK: true, Message: fmt.Sprintf("stopped relay for session %s", in.SessionID)}, nil
	})
}
