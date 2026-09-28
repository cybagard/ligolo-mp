package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ttpreport/ligolo-mp/v2/internal/version"
)

// Transport selects how the MCP server talks to its host.
type Transport string

const (
	TransportStdio Transport = "stdio"
	TransportHTTP  Transport = "http"
)

// Options configures the MCP server.
type Options struct {
	// ConfigPath is the operator config JSON (required).
	ConfigPath string
	// Transport is "stdio" (default) or "http".
	Transport Transport
	// HTTPAddr is the listen address when Transport is HTTP.
	HTTPAddr string
	// EventBufferSize bounds the recent-activity ring buffer.
	EventBufferSize int
}

// Serve connects to the ligolo-mp server as an operator client and runs the
// read-only MCP server until ctx is cancelled (stdio) or the HTTP server stops.
func Serve(ctx context.Context, opts Options) error {
	if opts.ConfigPath == "" {
		return fmt.Errorf("operator config path is required")
	}
	if opts.Transport == "" {
		opts.Transport = TransportStdio
	}

	client, err := NewClient(opts.ConfigPath, opts.EventBufferSize)
	if err != nil {
		return err
	}

	if err := client.Connect(ctx); err != nil {
		return err
	}
	defer client.Close()

	slog.Info("connected to ligolo-mp",
		slog.String("operator", client.Name()),
		slog.Bool("admin", client.IsAdmin()),
	)

	// Feed the recent-activity resource from the server event stream.
	go client.consumeEvents(ctx)

	server := newServer(client)

	switch opts.Transport {
	case TransportStdio:
		slog.Info("starting MCP server", slog.String("transport", "stdio"))
		return server.Run(ctx, &mcp.StdioTransport{})
	case TransportHTTP:
		addr := opts.HTTPAddr
		if addr == "" {
			addr = "127.0.0.1:8080"
		}
		handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
			return server
		}, nil)
		httpServer := &http.Server{Addr: addr, Handler: handler}
		go func() {
			<-ctx.Done()
			_ = httpServer.Close()
		}()
		slog.Info("starting MCP server", slog.String("transport", "http"), slog.String("addr", addr))
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	default:
		return fmt.Errorf("unknown transport %q", opts.Transport)
	}
}

// newServer builds the MCP server and registers the read-only tool set and the
// events resource. Only the connected operator's privileges gate admin tools;
// write tools are out of scope for milestone M1.
func newServer(client *Client) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "ligolo-mp",
		Title:   "Ligolo-MP",
		Version: version.Version,
	}, nil)

	registerReconTools(server, client)
	registerSessionTools(server, client)
	if client.IsAdmin() {
		registerAdminReadTools(server, client)
	}
	registerEventsResource(server, client)

	return server
}

// registerEventsResource exposes recent server activity as an MCP resource.
func registerEventsResource(s *mcp.Server, c *Client) {
	s.AddResource(&mcp.Resource{
		Name:        "recent-events",
		URI:         eventsURI,
		Title:       "Recent Ligolo-MP activity",
		Description: "A rolling buffer of recent ligolo-mp server activity events (sessions connecting, relays starting, operators joining, etc.).",
		MIMEType:    "application/json",
	}, func(ctx context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		text, err := c.eventsResourceText()
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      eventsURI,
				MIMEType: "application/json",
				Text:     text,
			}},
		}, nil
	})
}
