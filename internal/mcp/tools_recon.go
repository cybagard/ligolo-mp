package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	pb "github.com/ttpreport/ligolo-mp/v2/protobuf"
)

// noInput is the input type for tools that take no arguments.
type noInput struct{}

// tracerouteInput is the input for the traceroute tool.
type tracerouteInput struct {
	IP string `json:"ip" jsonschema:"the target IP address to trace a route to"`
}

// readOnly returns tool annotations marking a tool as non-modifying.
func readOnly(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		Title:        title,
		ReadOnlyHint: true,
	}
}

// registerReconTools registers the always-available read-only recon tools.
func registerReconTools(s *mcp.Server, c *Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "ligolo_get_metadata",
		Description: "Return the connected operator's identity (name, admin flag) and the ligolo-mp server configuration (operator and agent listener addresses).",
		Annotations: readOnly("Get metadata"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, metadataOutput, error) {
		resp, err := c.Ligolo().GetMetadata(ctx, &pb.Empty{})
		if err != nil {
			return nil, metadataOutput{}, err
		}
		return nil, toMetadataOutput(resp), nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "ligolo_traceroute",
		Description: "Trace which ligolo-mp session/interface (if any) routes to a given IP address. Useful for recon: determining which pivot reaches a target.",
		Annotations: readOnly("Traceroute"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in tracerouteInput) (*mcp.CallToolResult, tracerouteOutput, error) {
		if in.IP == "" {
			return nil, tracerouteOutput{}, fmt.Errorf("ip is required")
		}
		resp, err := c.Ligolo().Traceroute(ctx, &pb.TracerouteReq{IP: in.IP})
		if err != nil {
			return nil, tracerouteOutput{}, err
		}
		return nil, toTracerouteOutput(in.IP, resp), nil
	})
}
