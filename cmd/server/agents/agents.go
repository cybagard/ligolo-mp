package agents

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log/slog"
	"net"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/ttpreport/ligolo-mp/v2/internal/certificate"
	"github.com/ttpreport/ligolo-mp/v2/internal/config"
	"github.com/ttpreport/ligolo-mp/v2/internal/events"
	"github.com/ttpreport/ligolo-mp/v2/internal/session"
)

type AgentApiHandler struct {
	config         *config.Config
	certService    *certificate.CertificateService
	sessionService *session.SessionService

	connections chan net.Conn
	quit        chan error
}

func Run(config *config.Config, certService *certificate.CertificateService, sessionService *session.SessionService) error {
	CACert, err := certService.GetCA()
	if err != nil {
		return err
	}
	if CACert == nil {
		return errors.New("CA certificate not found")
	}
	certpool, err := CACert.CertPool()
	if err != nil {
		return err
	}

	agentCert, err := certService.GetAgentServerCert()
	if err != nil {
		return err
	}
	if agentCert == nil {
		return errors.New("agent server certificate not found")
	}
	tlsCert, err := agentCert.KeyPair()
	if err != nil {
		return err
	}

	handler := &AgentApiHandler{
		config:         config,
		certService:    certService,
		sessionService: sessionService,
		connections:    make(chan net.Conn, 4096),
		quit:           make(chan error, 1),
	}

	var clientAuth = tls.RequireAndVerifyClientCert
	if config.InsecureAgents {
		clientAuth = tls.NoClientCert
	}

	tlsConfig := &tls.Config{
		ClientAuth:         clientAuth,
		Certificates:       []tls.Certificate{tlsCert},
		ClientCAs:          certpool,
		RootCAs:            certpool,
		MinVersion:         tls.VersionTLS13,
		MaxVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			cert, err := x509.ParseCertificate(rawCerts[0])
			if err != nil {
				return err
			}

			options := x509.VerifyOptions{
				Roots: certpool,
			}
			if options.Roots == nil {
				return errors.New("no root certificate")
			}

			if _, err := cert.Verify(options); err != nil {
				return err
			}

			return nil
		},
	}

	go handler.serve("tcp", config.ListenInterface, tlsConfig)

	return <-handler.quit
}

func (aah *AgentApiHandler) serve(protocol string, listenIface string, tlsConfig *tls.Config) {
	defer func() { aah.quit <- nil }()

	server, err := tls.Listen(protocol, listenIface, tlsConfig)
	if err != nil {
		slog.Error("Could not start agent server",
			slog.Any("error", err),
		)
		return
	}
	defer server.Close()

	slog.Info("Agent server started",
		slog.Any("address", listenIface),
	)

	err = aah.sessionService.CleanUp()
	if err != nil {
		slog.Error("Could not clean up sessions",
			slog.Any("error", err),
		)
	}

	go aah.startHandler()

	for {
		conn, err := server.Accept()
		if err != nil {
			slog.Error("Agent server encountered an error",
				slog.Any("error", err),
			)

			if err == net.ErrClosed {
				return
			}

			continue
		}
		aah.connections <- conn
	}
}

func (aah *AgentApiHandler) startHandler() {
	for {
		remoteConn := <-aah.connections
		slog.Debug("agent connection received")

		// Resolve a stable agent identity from the mTLS client certificate
		// before multiplexing. This survives VM reverts and NIC changes; an
		// empty value falls back to the legacy interface-MAC identity.
		agentID := agentIdentity(remoteConn)

		config := yamux.DefaultConfig()
		config.LogOutput = io.Discard
		// Tighten keepalive so a dead agent (e.g. a hard-reverted VM that never
		// sent a TCP FIN) is detected and its session torn down promptly,
		// freeing the identity for reconnect. Worst-case detection latency is
		// roughly KeepAliveInterval + ConnectionWriteTimeout.
		if aah.config.AgentKeepAliveInterval > 0 {
			config.KeepAliveInterval = aah.config.AgentKeepAliveInterval
		}
		if aah.config.AgentConnectionWriteTimeout > 0 {
			config.ConnectionWriteTimeout = aah.config.AgentConnectionWriteTimeout
		}
		yamuxConn, err := yamux.Client(remoteConn, config)
		if err != nil {
			slog.Error("could not open multiplexed connection with agent")
			continue
		}
		slog.Debug("established multiplexed connection with agent")

		newSession, err := aah.sessionService.NewSession(yamuxConn, agentID)
		if err != nil {
			slog.Error("could not initialize new session", slog.Any("error", err))
			yamuxConn.Close()
			continue
		}
		slog.Debug("new session created", slog.Any("session", newSession))

		go aah.startSessionMonitor(newSession)

		slog.Debug("session initialized")

		events.Publish(events.OK, "new session with '%s' established", newSession.GetName())
	}

}

func (aah *AgentApiHandler) startSessionMonitor(sess *session.Session) {
	tick := time.NewTicker(1 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			// If this ID's live session is no longer this instance, we have
			// been superseded by a reconnecting agent; stop monitoring the
			// ghost so we do not touch the replacement.
			if aah.sessionService.GetSession(sess.ID) != sess {
				slog.Debug("session superseded, stopping monitor", slog.Any("session", sess))
				return
			}
			aah.sessionService.UpdateLastSeen(sess.ID)
		case <-sess.Multiplex.CloseChan():
			slog.Debug("session multiplexer closed", slog.Any("session", sess))
			// Only tear down if we are still the live session for this ID. A
			// superseded ghost's transport also closes here, but the live
			// session now belongs to the reconnecting agent and must be left
			// intact.
			if aah.sessionService.GetSession(sess.ID) != sess {
				slog.Debug("superseded session multiplexer closed, ignoring", slog.Any("session", sess))
				return
			}
			aah.sessionService.DisconnectSession(sess.ID)
			events.Publish(events.ERROR, "session with '%s' disconnected", sess.GetName())
			return
		}
	}

}

func (aah *AgentApiHandler) Close() {
	aah.quit <- nil
}

// agentIdentity derives a stable identity string for an agent from its mTLS
// client certificate. Each generated agent binary embeds a unique client
// certificate (with a random 128-bit serial), so this identity travels with
// the binary and remains constant across host changes that alter network
// interface MAC addresses — most notably VM snapshot reverts. It returns an
// empty string when no client certificate is presented (e.g. insecure-agent
// mode), in which case the caller falls back to the legacy MAC-based identity.
func agentIdentity(conn net.Conn) string {
	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return ""
	}

	// The peer certificate is only populated once the handshake completes.
	// Handshake is idempotent, so triggering it here is safe even though yamux
	// would otherwise drive it implicitly on first use.
	if err := tlsConn.Handshake(); err != nil {
		slog.Debug("agent TLS handshake failed while resolving identity", slog.Any("error", err))
		return ""
	}

	certs := tlsConn.ConnectionState().PeerCertificates
	if len(certs) == 0 || certs[0].SerialNumber == nil {
		return ""
	}

	// Serial number is unique per generated agent certificate; base16 keeps it
	// compact and stable.
	return certs[0].SerialNumber.Text(16)
}
