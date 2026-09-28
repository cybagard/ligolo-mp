// Package restgw is a read-only HTTP/JSON gateway over the ligolo-mp operator
// API (milestone M5). It exposes the read RPCs (metadata, sessions, traceroute)
// as plain JSON endpoints for dashboards and scripts. It is read-only by
// construction: no state-changing RPC is reachable, and it authenticates as an
// ordinary operator, so it can never exceed that operator's access.
package restgw

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	pb "github.com/ttpreport/ligolo-mp/v2/protobuf"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var marshaler = protojson.MarshalOptions{
	UseProtoNames:   false,
	EmitUnpopulated: true,
	Indent:          "  ",
}

// Handler builds the read-only HTTP mux backed by the given ligolo client.
func Handler(client pb.LigoloClient) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	mux.HandleFunc("/api/metadata", func(w http.ResponseWriter, r *http.Request) {
		if !requireGET(w, r) {
			return
		}
		resp, err := client.GetMetadata(r.Context(), &pb.Empty{})
		writeProto(w, resp, err)
	})

	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		if !requireGET(w, r) {
			return
		}
		resp, err := client.GetSessions(r.Context(), &pb.Empty{})
		writeProto(w, resp, err)
	})

	mux.HandleFunc("/api/traceroute", func(w http.ResponseWriter, r *http.Request) {
		if !requireGET(w, r) {
			return
		}
		ip := r.URL.Query().Get("ip")
		if ip == "" {
			writeError(w, http.StatusBadRequest, "query parameter 'ip' is required")
			return
		}
		resp, err := client.Traceroute(r.Context(), &pb.TracerouteReq{IP: ip})
		writeProto(w, resp, err)
	})

	return mux
}

// Serve runs the gateway on addr until ctx is cancelled.
func Serve(ctx context.Context, client pb.LigoloClient, addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           Handler(client),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	slog.Info("starting REST gateway (read-only)", slog.String("addr", addr))
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func requireGET(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "only GET is supported")
		return false
	}
	return true
}

func writeProto(w http.ResponseWriter, msg proto.Message, err error) {
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	out, merr := marshaler.Marshal(msg)
	if merr != nil {
		writeError(w, http.StatusInternalServerError, merr.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
