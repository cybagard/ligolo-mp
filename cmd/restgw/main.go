// Command ligolo-mp-restgw is a read-only HTTP/JSON gateway over the ligolo-mp
// operator API (milestone M5). It connects to a ligolo-mp server as an ordinary
// operator (using an exported operator config) and serves the read RPCs
// (metadata, sessions, traceroute) as JSON for dashboards. It exposes no
// state-changing endpoint.
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
	"github.com/ttpreport/ligolo-mp/v2/internal/restgw"
	"github.com/ttpreport/ligolo-mp/v2/internal/version"
)

func main() {
	var (
		configPath  = flag.String("config", "", "operator config JSON (exported from ligolo-mp; required)")
		addr        = flag.String("addr", "127.0.0.1:8081", "listen address for the read-only HTTP gateway")
		verbose     = flag.Bool("v", false, "verbose logging")
		showVersion = flag.Bool("version", false, "print version and exit")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "ligolo-mp-restgw %s (read-only)\n\nUsage:\n", version.Version)
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Printf("ligolo-mp-restgw %s\n", version.Version)
		return
	}

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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	client, err := mcp.NewClient(*configPath, 1)
	if err != nil {
		slog.Error("could not load operator config", slog.Any("error", err))
		os.Exit(1)
	}
	if err := client.Connect(ctx); err != nil {
		slog.Error("could not connect to ligolo-mp server", slog.Any("error", err))
		os.Exit(1)
	}
	defer client.Close()

	slog.Info("connected to ligolo-mp", slog.String("operator", client.Name()))

	if err := restgw.Serve(ctx, client.Ligolo(), *addr); err != nil {
		slog.Error("rest gateway exited with error", slog.Any("error", err))
		os.Exit(1)
	}
}
