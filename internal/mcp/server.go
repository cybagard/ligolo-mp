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
	// AllowWrites registers state-changing/destructive tools (relay, routing,
	// redirectors, rename/kill). Off by default: an MCP host with the default
	// configuration can observe but not change the engagement.
	AllowWrites bool
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

	server := newServer(client, opts.AllowWrites)

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

// newServer builds the MCP server and registers the tool set and the events
// resource. Read tools are always registered; admin read tools only when the
// connected operator is an admin; state-changing tools only when allowWrites is
// set. Admin *write* tools remain out of scope until milestone M3.
func newServer(client *Client, allowWrites bool) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "ligolo-mp",
		Title:   "Ligolo-MP",
		Version: version.Version,
	}, nil)

	registerReconTools(server, client)
	registerSessionReadTools(server, client)
	if client.IsAdmin() {
		registerAdminReadTools(server, client)
	}

	if allowWrites {
		registerSessionWriteTools(server, client)
		registerRouteTools(server, client)
		registerRedirectorTools(server, client)
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
