package restgw

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pb "github.com/ttpreport/ligolo-mp/v2/protobuf"
	"google.golang.org/grpc"
)

type fakeLigolo struct {
	pb.LigoloClient
	lastTraceIP string
}

func (f *fakeLigolo) GetMetadata(_ context.Context, _ *pb.Empty, _ ...grpc.CallOption) (*pb.GetMetadataResp, error) {
	return &pb.GetMetadataResp{
		Operator: &pb.Operator{Name: "tester", IsAdmin: true},
		Config:   &pb.Config{OperatorServer: "0.0.0.0:58008", AgentServer: "0.0.0.0:11601"},
	}, nil
}
func (f *fakeLigolo) GetSessions(_ context.Context, _ *pb.Empty, _ ...grpc.CallOption) (*pb.GetSessionsResp, error) {
	return &pb.GetSessionsResp{Sessions: []*pb.Session{{ID: "abc", Hostname: "victim01"}}}, nil
}
func (f *fakeLigolo) Traceroute(_ context.Context, in *pb.TracerouteReq, _ ...grpc.CallOption) (*pb.TracerouteResp, error) {
	f.lastTraceIP = in.GetIP()
	return &pb.TracerouteResp{Trace: []*pb.Traceroute{{IsInternal: true, Session: "victim01"}}}, nil
}

func newTestServer(t *testing.T, client pb.LigoloClient) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(Handler(client))
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestSessionsEndpoint(t *testing.T) {
	srv := newTestServer(t, &fakeLigolo{})
	code, body := get(t, srv.URL+"/api/sessions")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", code, body)
	}
	if !strings.Contains(body, "victim01") {
		t.Errorf("sessions body missing hostname: %s", body)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
}

func TestMetadataEndpoint(t *testing.T) {
	srv := newTestServer(t, &fakeLigolo{})
	code, body := get(t, srv.URL+"/api/metadata")
	if code != http.StatusOK || !strings.Contains(body, "tester") {
		t.Errorf("metadata unexpected: %d %s", code, body)
	}
}

func TestTracerouteRequiresIP(t *testing.T) {
	srv := newTestServer(t, &fakeLigolo{})
	code, _ := get(t, srv.URL+"/api/traceroute")
	if code != http.StatusBadRequest {
		t.Errorf("missing ip should be 400, got %d", code)
	}
}

func TestTraceroutePassesIP(t *testing.T) {
	fake := &fakeLigolo{}
	srv := newTestServer(t, fake)
	code, body := get(t, srv.URL+"/api/traceroute?ip=10.0.0.9")
	if code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", code, body)
	}
	if fake.lastTraceIP != "10.0.0.9" {
		t.Errorf("ip not propagated: %q", fake.lastTraceIP)
	}
}

func TestReadOnly_RejectsNonGET(t *testing.T) {
	srv := newTestServer(t, &fakeLigolo{})
	for _, method := range []string{http.MethodPost, http.MethodDelete, http.MethodPut} {
		req, _ := http.NewRequest(method, srv.URL+"/api/sessions", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s should be 405, got %d", method, resp.StatusCode)
		}
	}
}

func TestHealthz(t *testing.T) {
	srv := newTestServer(t, &fakeLigolo{})
	code, body := get(t, srv.URL+"/healthz")
	if code != http.StatusOK || !strings.Contains(body, "ok") {
		t.Errorf("healthz unexpected: %d %s", code, body)
	}
}
