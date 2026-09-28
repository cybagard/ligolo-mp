package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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

// --- Admin write tools (milestone M3) ---

type addOperatorInput struct {
	Name    string `json:"name" jsonschema:"the new operator's name"`
	IsAdmin bool   `json:"is_admin,omitempty" jsonschema:"grant admin privileges to the new operator"`
	Server  string `json:"server" jsonschema:"the operator gRPC server address the operator connects to, host:port"`
}

type operatorNameInput struct {
	Name string `json:"name" jsonschema:"the operator name"`
}

type certNameInput struct {
	Name string `json:"name" jsonschema:"the certificate name to regenerate (e.g. an operator or agent-server cert name)"`
}

type generateAgentInput struct {
	GOOS           string   `json:"goos" jsonschema:"target OS, e.g. linux, windows, darwin, freebsd"`
	GOARCH         string   `json:"goarch" jsonschema:"target architecture, e.g. amd64, 386, arm64"`
	Servers        []string `json:"servers" jsonschema:"one or more agent-server addresses (host:port) the agent should connect back to"`
	Obfuscate      bool     `json:"obfuscate,omitempty" jsonschema:"build an obfuscated agent binary"`
	ProxyServer    string   `json:"proxy_server,omitempty" jsonschema:"optional upstream proxy the agent should route through"`
	IgnoreEnvProxy bool     `json:"ignore_env_proxy,omitempty" jsonschema:"make the agent ignore environment proxy variables"`
}

type generateAgentOutput struct {
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	GOOS   string `json:"goos"`
	GOARCH string `json:"goarch"`
}

// registerAdminWriteTools registers admin write tools. Called only when the
// operator is admin AND --allow-admin is set. agentOut is the directory where
// generated agent binaries are written; empty disables ligolo_generate_agent.
func registerAdminWriteTools(s *mcp.Server, c *Client, agentOut string) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "ligolo_add_operator",
		Description: "Create a new operator on the server. The server generates the operator's certificate. State-changing; admin-only.",
		Annotations: writeAction("Add operator", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in addOperatorInput) (*mcp.CallToolResult, actionOutput, error) {
		if in.Name == "" || in.Server == "" {
			return nil, actionOutput{}, fmt.Errorf("name and server are required")
		}
		req := &pb.AddOperatorReq{Operator: &pb.Operator{Name: in.Name, IsAdmin: in.IsAdmin, Server: in.Server}}
		if _, err := c.Ligolo().AddOperator(ctx, req); err != nil {
			return nil, actionOutput{}, err
		}
		return nil, actionOutput{OK: true, Message: fmt.Sprintf("created operator %q (admin=%t)", in.Name, in.IsAdmin)}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "ligolo_del_operator",
		Description: "Delete an operator and revoke its certificate. Destructive; admin-only.",
		Annotations: writeAction("Delete operator", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in operatorNameInput) (*mcp.CallToolResult, actionOutput, error) {
		if in.Name == "" {
			return nil, actionOutput{}, fmt.Errorf("name is required")
		}
		if _, err := c.Ligolo().DelOperator(ctx, &pb.DelOperatorReq{Name: in.Name}); err != nil {
			return nil, actionOutput{}, err
		}
		return nil, actionOutput{OK: true, Message: fmt.Sprintf("deleted operator %q", in.Name)}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "ligolo_promote_operator",
		Description: "Grant admin privileges to an operator. Privilege change; admin-only.",
		Annotations: writeAction("Promote operator", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in operatorNameInput) (*mcp.CallToolResult, actionOutput, error) {
		if in.Name == "" {
			return nil, actionOutput{}, fmt.Errorf("name is required")
		}
		if _, err := c.Ligolo().PromoteOperator(ctx, &pb.PromoteOperatorReq{Name: in.Name}); err != nil {
			return nil, actionOutput{}, err
		}
		return nil, actionOutput{OK: true, Message: fmt.Sprintf("promoted operator %q to admin", in.Name)}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "ligolo_demote_operator",
		Description: "Revoke admin privileges from an operator. Privilege change; admin-only.",
		Annotations: writeAction("Demote operator", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in operatorNameInput) (*mcp.CallToolResult, actionOutput, error) {
		if in.Name == "" {
			return nil, actionOutput{}, fmt.Errorf("name is required")
		}
		if _, err := c.Ligolo().DemoteOperator(ctx, &pb.DemoteOperatorReq{Name: in.Name}); err != nil {
			return nil, actionOutput{}, err
		}
		return nil, actionOutput{OK: true, Message: fmt.Sprintf("demoted operator %q", in.Name)}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "ligolo_regen_cert",
		Description: "Regenerate a certificate by name. Destructive (invalidates the previous certificate); admin-only.",
		Annotations: writeAction("Regenerate certificate", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in certNameInput) (*mcp.CallToolResult, actionOutput, error) {
		if in.Name == "" {
			return nil, actionOutput{}, fmt.Errorf("name is required")
		}
		if _, err := c.Ligolo().RegenCert(ctx, &pb.RegenCertReq{Name: in.Name}); err != nil {
			return nil, actionOutput{}, err
		}
		return nil, actionOutput{OK: true, Message: fmt.Sprintf("regenerated certificate %q", in.Name)}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "ligolo_generate_agent",
		Description: "Generate an agent binary for a target platform and write it to the server's configured agent output directory. " +
			"Returns the file path and size; the binary bytes are never returned inline. Admin-only.",
		Annotations: writeAction("Generate agent", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in generateAgentInput) (*mcp.CallToolResult, generateAgentOutput, error) {
		if agentOut == "" {
			return nil, generateAgentOutput{}, fmt.Errorf("agent generation is disabled: start the MCP server with --agent-out <dir>")
		}
		if in.GOOS == "" || in.GOARCH == "" {
			return nil, generateAgentOutput{}, fmt.Errorf("goos and goarch are required")
		}
		if len(in.Servers) == 0 {
			return nil, generateAgentOutput{}, fmt.Errorf("at least one server address is required")
		}

		req := &pb.GenerateAgentReq{
			Servers:        strings.Join(in.Servers, "\n"),
			GOOS:           in.GOOS,
			GOARCH:         in.GOARCH,
			Obfuscate:      in.Obfuscate,
			ProxyServer:    in.ProxyServer,
			IgnoreEnvProxy: in.IgnoreEnvProxy,
		}
		resp, err := c.Ligolo().GenerateAgent(ctx, req)
		if err != nil {
			return nil, generateAgentOutput{}, err
		}

		path, err := writeAgentBinary(agentOut, in.GOOS, in.GOARCH, resp.GetAgentBinary())
		if err != nil {
			return nil, generateAgentOutput{}, err
		}
		return nil, generateAgentOutput{
			Path:   path,
			Bytes:  len(resp.GetAgentBinary()),
			GOOS:   in.GOOS,
			GOARCH: in.GOARCH,
		}, nil
	})
}

// writeAgentBinary saves an agent binary to dir with a descriptive, unique
// filename and returns the path.
func writeAgentBinary(dir, goos, goarch string, data []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating agent output dir: %w", err)
	}
	name := fmt.Sprintf("ligolo-mp-agent_%s_%s_%d", goos, goarch, time.Now().Unix())
	if goos == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("writing agent binary: %w", err)
	}
	return path, nil
}
