package config

import (
	"os"
	"os/user"
	"path"
	"path/filepath"
	"time"

	pb "github.com/ttpreport/ligolo-mp/v2/protobuf"
)

type Config struct {
	Environment          string
	Verbose              bool
	ListenInterface      string
	MaxInFlight          int
	MaxConnectionHandler int
	OperatorAddr         string
	InsecureAgents       bool

	// AgentKeepAliveInterval is how often the server sends a yamux keepalive
	// ping to each agent. A dead agent (e.g. a VM that was hard-reverted and
	// never sent a TCP FIN) is only detected via these pings, so a shorter
	// interval frees the stale session --- and its identity --- for reconnect
	// sooner. A value <= 0 keeps the yamux default.
	AgentKeepAliveInterval time.Duration

	// AgentConnectionWriteTimeout bounds how long a write (including a
	// keepalive ping awaiting its pong) may stall before the connection is
	// considered dead. It must comfortably exceed the round-trip latency of
	// the slowest pivot the agent tunnels through, or healthy-but-slow
	// sessions will be dropped. A value <= 0 keeps the yamux default.
	AgentConnectionWriteTimeout time.Duration
}

func (cfg *Config) GetRootAppDir() string {
	user, _ := user.Current()
	dir := filepath.Join(user.HomeDir, ".ligolo-mp-"+cfg.Environment)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err = os.MkdirAll(dir, 0700)
		if err != nil {
			panic(err)
		}
	}
	return dir
}

func (cfg *Config) GetAssetsDir() string {
	dir := path.Join(cfg.GetRootAppDir(), "assets")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err = os.MkdirAll(dir, 0700)
		if err != nil {
			panic(err)
		}
	}
	return dir
}

func (cfg *Config) GetStorageDir() string {
	dir := path.Join(cfg.GetRootAppDir(), "storage")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err = os.MkdirAll(dir, 0700)
		if err != nil {
			panic(err)
		}
	}
	return dir
}

func (cfg *Config) Proto() *pb.Config {
	return &pb.Config{
		OperatorServer: cfg.OperatorAddr,
		AgentServer:    cfg.ListenInterface,
	}
}

func ProtoToConfig(p *pb.Config) *Config {
	return &Config{
		OperatorAddr:    p.OperatorServer,
		ListenInterface: p.AgentServer,
	}
}
