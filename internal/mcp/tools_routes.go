package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	pb "github.com/ttpreport/ligolo-mp/v2/protobuf"
)

type addRouteInput struct {
	SessionID  string `json:"session_id" jsonschema:"the session that should route this CIDR"`
	Cidr       string `json:"cidr" jsonschema:"the network CIDR to route through the session, e.g. 10.0.0.0/24"`
	Metric     int32  `json:"metric,omitempty" jsonschema:"optional route metric (priority); lower wins"`
	IsLoopback bool   `json:"is_loopback,omitempty" jsonschema:"route to the loopback of the target machine"`
}

type editRouteInput struct {
	SessionID  string `json:"session_id" jsonschema:"the session owning the route"`
	RouteID    string `json:"route_id" jsonschema:"the route ID to edit (from ligolo_list_sessions)"`
	Cidr       string `json:"cidr" jsonschema:"the new network CIDR"`
	Metric     int32  `json:"metric,omitempty" jsonschema:"optional route metric (priority); lower wins"`
	IsLoopback bool   `json:"is_loopback,omitempty" jsonschema:"route to the loopback of the target machine"`
}

type moveRouteInput struct {
	OldSessionID string `json:"old_session_id" jsonschema:"the session that currently owns the route"`
	RouteID      string `json:"route_id" jsonschema:"the route ID to move"`
	NewSessionID string `json:"new_session_id" jsonschema:"the session to move the route to"`
}

type delRouteInput struct {
	SessionID string `json:"session_id" jsonschema:"the session owning the route"`
	RouteID   string `json:"route_id" jsonschema:"the route ID to delete"`
}

// registerRouteTools registers state-changing routing tools. Registered only
// when --allow-writes is set.
func registerRouteTools(s *mcp.Server, c *Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "ligolo_add_route",
		Description: "Add a network route (CIDR) to a session so traffic to that network is pivoted through the agent. State-changing (modifies local network routing).",
		Annotations: writeAction("Add route", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in addRouteInput) (*mcp.CallToolResult, actionOutput, error) {
		if in.SessionID == "" || in.Cidr == "" {
			return nil, actionOutput{}, fmt.Errorf("session_id and cidr are required")
		}
		req := &pb.AddRouteReq{
			SessionID: in.SessionID,
			Route:     &pb.Route{Cidr: in.Cidr, Metric: in.Metric, IsLoopback: in.IsLoopback},
		}
		if _, err := c.Ligolo().AddRoute(ctx, req); err != nil {
			return nil, actionOutput{}, err
		}
		return nil, actionOutput{OK: true, Message: fmt.Sprintf("added route %s to session %s", in.Cidr, in.SessionID)}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "ligolo_edit_route",
		Description: "Replace an existing route's CIDR/metric/loopback settings. State-changing.",
		Annotations: writeAction("Edit route", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in editRouteInput) (*mcp.CallToolResult, actionOutput, error) {
		if in.SessionID == "" || in.RouteID == "" || in.Cidr == "" {
			return nil, actionOutput{}, fmt.Errorf("session_id, route_id and cidr are required")
		}
		req := &pb.EditRouteReq{
			SessionID: in.SessionID,
			RouteID:   in.RouteID,
			Route:     &pb.Route{Cidr: in.Cidr, Metric: in.Metric, IsLoopback: in.IsLoopback},
		}
		if _, err := c.Ligolo().EditRoute(ctx, req); err != nil {
			return nil, actionOutput{}, err
		}
		return nil, actionOutput{OK: true, Message: fmt.Sprintf("edited route %s on session %s", in.RouteID, in.SessionID)}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "ligolo_move_route",
		Description: "Move a route from one session to another. State-changing.",
		Annotations: writeAction("Move route", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in moveRouteInput) (*mcp.CallToolResult, actionOutput, error) {
		if in.OldSessionID == "" || in.RouteID == "" || in.NewSessionID == "" {
			return nil, actionOutput{}, fmt.Errorf("old_session_id, route_id and new_session_id are required")
		}
		req := &pb.MoveRouteReq{OldSessionID: in.OldSessionID, RouteID: in.RouteID, NewSessionID: in.NewSessionID}
		if _, err := c.Ligolo().MoveRoute(ctx, req); err != nil {
			return nil, actionOutput{}, err
		}
		return nil, actionOutput{OK: true, Message: fmt.Sprintf("moved route %s from %s to %s", in.RouteID, in.OldSessionID, in.NewSessionID)}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "ligolo_del_route",
		Description: "Delete a route from a session. Destructive (removes network routing).",
		Annotations: writeAction("Delete route", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in delRouteInput) (*mcp.CallToolResult, actionOutput, error) {
		if in.SessionID == "" || in.RouteID == "" {
			return nil, actionOutput{}, fmt.Errorf("session_id and route_id are required")
		}
		req := &pb.DelRouteReq{SessionID: in.SessionID, RouteID: in.RouteID}
		if _, err := c.Ligolo().DelRoute(ctx, req); err != nil {
			return nil, actionOutput{}, err
		}
		return nil, actionOutput{OK: true, Message: fmt.Sprintf("deleted route %s from session %s", in.RouteID, in.SessionID)}, nil
	})
}
