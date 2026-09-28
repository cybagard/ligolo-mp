// Command ligolo-mp-mcp is a read-only Model Context Protocol (MCP) server for
// Ligolo-MP. It connects to a running ligolo-mp server as an ordinary operator
// (using an exported operator config) and exposes the server's read RPCs as MCP
// tools, so an MCP host (Claude Code, etc.) can observe pivoting operations.
//
// It changes nothing on the server and can do nothing an operator with the same
// certificate could not already do. This build (milestone M1) is read-only.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ttpreport/ligolo-mp/v2/internal/mcp"
	"github.com/ttpreport/ligolo-mp/v2/internal/version"
)

func main() {
	var (
		configPath  = flag.String("config", "", "operator config JSON (exported from ligolo-mp; required)")
		transport   = flag.String("transport", "stdio", "MCP transport: stdio or http")
		httpAddr    = flag.String("http-addr", "127.0.0.1:8080", "listen address when -transport=http")
		bufferSize  = flag.Int("event-buffer", 200, "number of recent activity events to retain for the events resource")
		verbose     = flag.Bool("v", false, "verbose logging")
		allowWrites = flag.Bool("allow-writes", false, "register state-changing tools (relay, routing, redirectors, rename/kill); off by default")
		allowAdmin  = flag.Bool("allow-admin", false, "register admin write tools (operator/cert management, agent generation); requires an admin operator; off by default")
		agentOut    = flag.String("agent-out", "", "directory where ligolo_generate_agent writes agent binaries (required to enable that tool)")
		showVersion = flag.Bool("version", false, "print version and exit")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "ligolo-mp-mcp %s (read-only)\n\nUsage:\n", version.Version)
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Printf("ligolo-mp-mcp %s\n", version.Version)
		return
	}

	// Logs MUST go to stderr: on the stdio transport, stdout carries the
	// JSON-RPC protocol stream and any stray write to it corrupts the session.
	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "error: -config is required")
		flag.Usage()
		os.Exit(2)
	}
	if *allowWrites {
		slog.Warn("write tools enabled: this MCP server can change the engagement (relay, routing, redirectors, kill)")
	}
	if *allowAdmin {
		slog.Warn("admin write tools enabled: this MCP server can manage operators, regenerate certs, and generate agents (admin operator only)")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	opts := mcp.Options{
		ConfigPath:      *configPath,
		Transport:       mcp.Transport(*transport),
		HTTPAddr:        *httpAddr,
		EventBufferSize: *bufferSize,
		AllowWrites:     *allowWrites,
		AllowAdmin:      *allowAdmin,
		AgentOut:        *agentOut,
	}

	if err := mcp.Serve(ctx, opts); err != nil {
		slog.Error("mcp server exited with error", slog.Any("error", err))
		os.Exit(1)
	}
}
