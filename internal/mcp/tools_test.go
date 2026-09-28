package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	pb "github.com/ttpreport/ligolo-mp/v2/protobuf"
	"google.golang.org/grpc"
)

// fakeLigolo implements pb.LigoloClient. Only the read methods used by the M1
// tools are overridden; the embedded nil interface makes any unexpected call
// panic, which is what we want in a test.
type fakeLigolo struct {
	pb.LigoloClient

	metadata  *pb.GetMetadataResp
	sessions  *pb.GetSessionsResp
	trace     *pb.TracerouteResp
	operators *pb.GetOperatorsResp
	certs     *pb.GetCertsResp

	lastTraceIP string
}

func (f *fakeLigolo) GetMetadata(_ context.Context, _ *pb.Empty, _ ...grpc.CallOption) (*pb.GetMetadataResp, error) {
	return f.metadata, nil
}
func (f *fakeLigolo) GetSessions(_ context.Context, _ *pb.Empty, _ ...grpc.CallOption) (*pb.GetSessionsResp, error) {
	return f.sessions, nil
}
func (f *fakeLigolo) Traceroute(_ context.Context, in *pb.TracerouteReq, _ ...grpc.CallOption) (*pb.TracerouteResp, error) {
	f.lastTraceIP = in.GetIP()
	return f.trace, nil
}
func (f *fakeLigolo) GetOperators(_ context.Context, _ *pb.Empty, _ ...grpc.CallOption) (*pb.GetOperatorsResp, error) {
	return f.operators, nil
}
func (f *fakeLigolo) GetCerts(_ context.Context, _ *pb.Empty, _ ...grpc.CallOption) (*pb.GetCertsResp, error) {
	return f.certs, nil
}

func newTestClient(l pb.LigoloClient, isAdmin bool) *Client {
	return &Client{
		ligolo:  l,
		name:    "tester",
		isAdmin: isAdmin,
		events:  newEventBuffer(10),
	}
}

func sampleFake() *fakeLigolo {
	return &fakeLigolo{
		metadata: &pb.GetMetadataResp{
			Operator: &pb.Operator{Name: "tester", IsAdmin: true},
			Config:   &pb.Config{OperatorServer: "0.0.0.0:58008", AgentServer: "0.0.0.0:11601"},
		},
		sessions: &pb.GetSessionsResp{Sessions: []*pb.Session{
			{
				ID:          "abc123",
				Hostname:    "victim01",
				IsConnected: true,
				IsRelaying:  true,
				Tun:         &pb.Tun{Name: "ligolo0", Routes: []*pb.Route{{ID: "r1", Cidr: "10.0.0.0/24", Metric: 100}}},
				Interfaces:  []*pb.Interface{{Name: "eth0", IPs: []string{"10.0.0.5/24"}}},
				Redirectors: []*pb.Redirector{{ID: "rd1", Protocol: "tcp", From: "0.0.0.0:9000", To: "10.0.0.9:22"}},
			},
		}},
		trace: &pb.TracerouteResp{Trace: []*pb.Traceroute{
			{IsInternal: true, Session: "victim01", Iface: "ligolo0", Metric: 100},
		}},
		operators: &pb.GetOperatorsResp{Operators: []*pb.Operator{
			{Name: "admin", IsAdmin: true, IsOnline: true},
			{Name: "bob", IsAdmin: false},
		}},
		certs: &pb.GetCertsResp{Certs: []*pb.Cert{
			// Key/Certificate bytes must never surface through MCP.
			{Name: "operator-admin", ExpiryDate: "2030-01-01", Key: []byte("SECRETKEY"), Certificate: []byte("CERTBYTES")},
		}},
	}
}

// connectTest wires a client session to a server built over newServer.
func connectTest(t *testing.T, client *Client) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := newServer(client)

	st, ct := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	cs, err := c.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func callText(t *testing.T, cs *mcp.ClientSession, name string, args any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("CallTool %s returned tool error: %v", name, res.Content)
	}
	var sb strings.Builder
	for _, ct := range res.Content {
		if tc, ok := ct.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

func TestListSessions(t *testing.T) {
	cs := connectTest(t, newTestClient(sampleFake(), true))
	out := callText(t, cs, "ligolo_list_sessions", noInput{})

	for _, want := range []string{"victim01", "10.0.0.0/24", "ligolo0", "0.0.0.0:9000"} {
		if !strings.Contains(out, want) {
			t.Errorf("list_sessions output missing %q; got: %s", want, out)
		}
	}
}

func TestGetMetadata(t *testing.T) {
	cs := connectTest(t, newTestClient(sampleFake(), true))
	out := callText(t, cs, "ligolo_get_metadata", noInput{})
	if !strings.Contains(out, "tester") || !strings.Contains(out, "11601") {
		t.Errorf("metadata output unexpected: %s", out)
	}
}

func TestTraceroutePassesIP(t *testing.T) {
	fake := sampleFake()
	cs := connectTest(t, newTestClient(fake, true))
	out := callText(t, cs, "ligolo_traceroute", tracerouteInput{IP: "10.0.0.9"})
	if fake.lastTraceIP != "10.0.0.9" {
		t.Errorf("traceroute IP not propagated: got %q", fake.lastTraceIP)
	}
	if !strings.Contains(out, "victim01") {
		t.Errorf("traceroute output unexpected: %s", out)
	}
}

func TestTracerouteRequiresIP(t *testing.T) {
	cs := connectTest(t, newTestClient(sampleFake(), true))
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "ligolo_traceroute", Arguments: tracerouteInput{}})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Error("expected tool error for empty IP, got success")
	}
}

func TestListCertsNeverLeaksKeys(t *testing.T) {
	cs := connectTest(t, newTestClient(sampleFake(), true))
	out := callText(t, cs, "ligolo_list_certs", noInput{})
	if strings.Contains(out, "SECRETKEY") || strings.Contains(out, "CERTBYTES") {
		t.Errorf("cert output leaked key/cert material: %s", out)
	}
	if !strings.Contains(out, "operator-admin") {
		t.Errorf("cert output missing name: %s", out)
	}
}

func TestAdminToolsGatedByPrivilege(t *testing.T) {
	// Admin operator: admin read tools present.
	adminTools := listToolNames(t, newTestClient(sampleFake(), true))
	for _, want := range []string{"ligolo_list_operators", "ligolo_list_certs"} {
		if !adminTools[want] {
			t.Errorf("admin tool %q missing for admin operator", want)
		}
	}

	// Non-admin operator: admin read tools absent, read tools still present.
	userTools := listToolNames(t, newTestClient(sampleFake(), false))
	for _, notWant := range []string{"ligolo_list_operators", "ligolo_list_certs"} {
		if userTools[notWant] {
			t.Errorf("admin tool %q must not be registered for non-admin operator", notWant)
		}
	}
	for _, want := range []string{"ligolo_list_sessions", "ligolo_get_metadata", "ligolo_traceroute"} {
		if !userTools[want] {
			t.Errorf("read tool %q missing for non-admin operator", want)
		}
	}
}

func listToolNames(t *testing.T, client *Client) map[string]bool {
	t.Helper()
	cs := connectTest(t, client)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := make(map[string]bool)
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	return names
}

func TestEventBufferRingAndSnapshot(t *testing.T) {
	b := newEventBuffer(3)
	for i := 0; i < 5; i++ {
		b.add(&pb.Event{Type: 0, Data: string(rune('a' + i))})
	}
	snap := b.snapshot()
	if len(snap) != 3 {
		t.Fatalf("ring size not respected: got %d, want 3", len(snap))
	}
	// Oldest retained should be "c" (a,b evicted).
	if snap[0].Data != "c" || snap[2].Data != "e" {
		t.Errorf("unexpected ring contents: %+v", snap)
	}
	if snap[0].Type != "INFO" {
		t.Errorf("event type label wrong: %q", snap[0].Type)
	}
}

func TestEventsResourceReadable(t *testing.T) {
	client := newTestClient(sampleFake(), false)
	client.events.add(&pb.Event{Type: 1, Data: "session with 'x' disconnected"})

	cs := connectTest(t, client)
	res, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: eventsURI})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(res.Contents) == 0 {
		t.Fatal("no resource contents returned")
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &parsed); err != nil {
		t.Fatalf("resource text not JSON: %v", err)
	}
	if !strings.Contains(res.Contents[0].Text, "disconnected") {
		t.Errorf("events resource missing event: %s", res.Contents[0].Text)
	}
}
