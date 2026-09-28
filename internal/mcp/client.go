// Package mcp implements a read-only Model Context Protocol (MCP) server for
// Ligolo-MP (milestone M1). It is a thin, auditable adapter that connects to a
// running ligolo-mp server as an ordinary operator gRPC client and exposes the
// server's read RPCs as MCP tools. It introduces no new capability and makes no
// changes to the ligolo-mp server or its security model: every call is
// authenticated and authorized server-side exactly as if it came from the TUI.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/ttpreport/ligolo-mp/v2/internal/operator"
	pb "github.com/ttpreport/ligolo-mp/v2/protobuf"
)

// Client wraps an operator connection and exposes the ligolo gRPC client to the
// MCP tool layer. It deliberately holds the pb.LigoloClient as an interface so
// the tool layer can be unit-tested against a mock without a live server.
type Client struct {
	oper    *operator.Operator
	ligolo  pb.LigoloClient
	name    string
	isAdmin bool
	events  *eventBuffer
}

// NewClient loads an operator config file (the JSON produced by the server's
// ExportOperator / imported by the TUI) but does not connect yet. The config
// file contains the operator's private key and is a sensitive credential: it
// grants exactly that operator's access, no more and no less.
func NewClient(configPath string, eventBufferSize int) (*Client, error) {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading operator config: %w", err)
	}

	var oper operator.Operator
	if err := json.Unmarshal(raw, &oper); err != nil {
		return nil, fmt.Errorf("parsing operator config: %w", err)
	}
	if oper.Cert == nil {
		return nil, fmt.Errorf("operator config %q has no certificate", configPath)
	}

	return &Client{
		oper:   &oper,
		events: newEventBuffer(eventBufferSize),
	}, nil
}

// Connect dials the ligolo-mp server over mTLS using the operator credential
// and resolves the operator's identity/privilege from the server (authoritative
// — not from the config file, which the operator could have edited). It must be
// called before the client is used.
func (c *Client) Connect(ctx context.Context) error {
	if err := c.oper.Connect(); err != nil {
		return fmt.Errorf("connecting to ligolo-mp server %q: %w", c.oper.Server, err)
	}
	c.ligolo = c.oper.Client()

	meta, err := c.ligolo.GetMetadata(ctx, &pb.Empty{})
	if err != nil {
		return fmt.Errorf("fetching operator metadata: %w", err)
	}
	if meta.GetOperator() != nil {
		c.name = meta.Operator.GetName()
		c.isAdmin = meta.Operator.GetIsAdmin()
	}

	return nil
}

// Ligolo returns the underlying gRPC client. gRPC transparently re-establishes
// the connection for unary calls, so read tools need no explicit reconnect.
func (c *Client) Ligolo() pb.LigoloClient { return c.ligolo }

// Name is the connected operator's name as reported by the server.
func (c *Client) Name() string { return c.name }

// IsAdmin reports whether the connected operator has admin privileges, as
// reported by the server. Admin-only read tools are registered only when true.
func (c *Client) IsAdmin() bool { return c.isAdmin }

// Close tears down the operator connection.
func (c *Client) Close() error {
	if c.oper == nil {
		return nil
	}
	return c.oper.Disconnect()
}
