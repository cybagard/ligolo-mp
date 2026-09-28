package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	pb "github.com/ttpreport/ligolo-mp/v2/protobuf"
)

type addRedirectorInput struct {
	SessionID string `json:"session_id" jsonschema:"the session to create the redirector on"`
	Protocol  string `json:"protocol" jsonschema:"the protocol, e.g. tcp or udp"`
	From      string `json:"from" jsonschema:"the listen address on the operator side, e.g. 0.0.0.0:9000"`
	To        string `json:"to" jsonschema:"the destination address reachable from the agent, e.g. 10.0.0.9:22"`
}

type delRedirectorInput struct {
	SessionID    string `json:"session_id" jsonschema:"the session owning the redirector"`
	RedirectorID string `json:"redirector_id" jsonschema:"the redirector ID to delete (from ligolo_list_sessions)"`
}

// registerRedirectorTools registers state-changing redirector tools. Registered
// only when --allow-writes is set.
func registerRedirectorTools(s *mcp.Server, c *Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "ligolo_add_redirector",
		Description: "Create a redirector (listener) on a session that forwards traffic from a local address to a destination reachable through the agent. State-changing.",
		Annotations: writeAction("Add redirector", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in addRedirectorInput) (*mcp.CallToolResult, actionOutput, error) {
		if in.SessionID == "" || in.Protocol == "" || in.From == "" || in.To == "" {
			return nil, actionOutput{}, fmt.Errorf("session_id, protocol, from and to are required")
		}
		req := &pb.AddRedirectorReq{SessionID: in.SessionID, Protocol: in.Protocol, From: in.From, To: in.To}
		if _, err := c.Ligolo().AddRedirector(ctx, req); err != nil {
			return nil, actionOutput{}, err
		}
		return nil, actionOutput{OK: true, Message: fmt.Sprintf("added %s redirector %s -> %s on session %s", in.Protocol, in.From, in.To, in.SessionID)}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "ligolo_del_redirector",
		Description: "Delete a redirector from a session. Destructive (tears down the listener).",
		Annotations: writeAction("Delete redirector", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in delRedirectorInput) (*mcp.CallToolResult, actionOutput, error) {
		if in.SessionID == "" || in.RedirectorID == "" {
			return nil, actionOutput{}, fmt.Errorf("session_id and redirector_id are required")
		}
		req := &pb.DelRedirectorReq{SessionID: in.SessionID, RedirectorID: in.RedirectorID}
		if _, err := c.Ligolo().DelRedirector(ctx, req); err != nil {
			return nil, actionOutput{}, err
		}
		return nil, actionOutput{OK: true, Message: fmt.Sprintf("deleted redirector %s from session %s", in.RedirectorID, in.SessionID)}, nil
	})
}
