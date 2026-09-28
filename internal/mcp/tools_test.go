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

// fakeLigolo implements pb.LigoloClient. Only the methods used by the MCP tools
// are overridden; the embedded nil interface makes any unexpected call panic,
// which is what we want in a test.
type fakeLigolo struct {
	pb.LigoloClient

	metadata  *pb.GetMetadataResp
	sessions  *pb.GetSessionsResp
	trace     *pb.TracerouteResp
	operators *pb.GetOperatorsResp
	certs     *pb.GetCertsResp

	lastTraceIP string

	// write-call capture
	renamed      *pb.RenameSessionReq
	killed       *pb.KillSessionReq
	started      *pb.StartRelayReq
	stopped      *pb.StopRelayReq
	addedRoute   *pb.AddRouteReq
	editedRoute  *pb.EditRouteReq
	movedRoute   *pb.MoveRouteReq
	deletedRoute *pb.DelRouteReq
	addedRedir   *pb.AddRedirectorReq
	deletedRedir *pb.DelRedirectorReq

	// admin write-call capture
	addedOperator   *pb.AddOperatorReq
	deletedOperator *pb.DelOperatorReq
	promoted        *pb.PromoteOperatorReq
	demoted         *pb.DemoteOperatorReq
	regenerated     *pb.RegenCertReq
	generatedAgent  *pb.GenerateAgentReq
	agentBinary     []byte
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

func (f *fakeLigolo) RenameSession(_ context.Context, in *pb.RenameSessionReq, _ ...grpc.CallOption) (*pb.Empty, error) {
	f.renamed = in
	return &pb.Empty{}, nil
}
func (f *fakeLigolo) KillSession(_ context.Context, in *pb.KillSessionReq, _ ...grpc.CallOption) (*pb.Empty, error) {
	f.killed = in
	return &pb.Empty{}, nil
}
func (f *fakeLigolo) StartRelay(_ context.Context, in *pb.StartRelayReq, _ ...grpc.CallOption) (*pb.Empty, error) {
	f.started = in
	return &pb.Empty{}, nil
}
func (f *fakeLigolo) StopRelay(_ context.Context, in *pb.StopRelayReq, _ ...grpc.CallOption) (*pb.Empty, error) {
	f.stopped = in
	return &pb.Empty{}, nil
}
func (f *fakeLigolo) AddRoute(_ context.Context, in *pb.AddRouteReq, _ ...grpc.CallOption) (*pb.Empty, error) {
	f.addedRoute = in
	return &pb.Empty{}, nil
}
func (f *fakeLigolo) EditRoute(_ context.Context, in *pb.EditRouteReq, _ ...grpc.CallOption) (*pb.Empty, error) {
	f.editedRoute = in
	return &pb.Empty{}, nil
}
func (f *fakeLigolo) MoveRoute(_ context.Context, in *pb.MoveRouteReq, _ ...grpc.CallOption) (*pb.Empty, error) {
	f.movedRoute = in
	return &pb.Empty{}, nil
}
func (f *fakeLigolo) DelRoute(_ context.Context, in *pb.DelRouteReq, _ ...grpc.CallOption) (*pb.Empty, error) {
	f.deletedRoute = in
	return &pb.Empty{}, nil
}
func (f *fakeLigolo) AddRedirector(_ context.Context, in *pb.AddRedirectorReq, _ ...grpc.CallOption) (*pb.Empty, error) {
	f.addedRedir = in
	return &pb.Empty{}, nil
}
func (f *fakeLigolo) DelRedirector(_ context.Context, in *pb.DelRedirectorReq, _ ...grpc.CallOption) (*pb.Empty, error) {
	f.deletedRedir = in
	return &pb.Empty{}, nil
}

func (f *fakeLigolo) AddOperator(_ context.Context, in *pb.AddOperatorReq, _ ...grpc.CallOption) (*pb.AddOperatorResp, error) {
	f.addedOperator = in
	return &pb.AddOperatorResp{Operator: in.GetOperator()}, nil
}
func (f *fakeLigolo) DelOperator(_ context.Context, in *pb.DelOperatorReq, _ ...grpc.CallOption) (*pb.Empty, error) {
	f.deletedOperator = in
	return &pb.Empty{}, nil
}
func (f *fakeLigolo) PromoteOperator(_ context.Context, in *pb.PromoteOperatorReq, _ ...grpc.CallOption) (*pb.Empty, error) {
	f.promoted = in
	return &pb.Empty{}, nil
}
func (f *fakeLigolo) DemoteOperator(_ context.Context, in *pb.DemoteOperatorReq, _ ...grpc.CallOption) (*pb.Empty, error) {
	f.demoted = in
	return &pb.Empty{}, nil
}
func (f *fakeLigolo) RegenCert(_ context.Context, in *pb.RegenCertReq, _ ...grpc.CallOption) (*pb.Empty, error) {
	f.regenerated = in
	return &pb.Empty{}, nil
}
func (f *fakeLigolo) GenerateAgent(_ context.Context, in *pb.GenerateAgentReq, _ ...grpc.CallOption) (*pb.GenerateAgentResp, error) {
	f.generatedAgent = in
	return &pb.GenerateAgentResp{AgentBinary: f.agentBinary}, nil
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
		agentBinary: []byte("FAKEELF"),
	}
}

// connectTest wires a client session to a server built over newServer.
func connectTest(t *testing.T, client *Client, cfg regConfig) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := newServer(client, cfg)

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

func listToolNames(t *testing.T, client *Client, cfg regConfig) map[string]bool {
	t.Helper()
	cs := connectTest(t, client, cfg)
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

// --- read tools (M1) ---

func TestListSessions(t *testing.T) {
	cs := connectTest(t, newTestClient(sampleFake(), true), regConfig{})
	out := callText(t, cs, "ligolo_list_sessions", noInput{})
	for _, want := range []string{"victim01", "10.0.0.0/24", "ligolo0", "0.0.0.0:9000"} {
		if !strings.Contains(out, want) {
			t.Errorf("list_sessions output missing %q; got: %s", want, out)
		}
	}
}

func TestGetMetadata(t *testing.T) {
	cs := connectTest(t, newTestClient(sampleFake(), true), regConfig{})
	out := callText(t, cs, "ligolo_get_metadata", noInput{})
	if !strings.Contains(out, "tester") || !strings.Contains(out, "11601") {
		t.Errorf("metadata output unexpected: %s", out)
	}
}

func TestTraceroutePassesIP(t *testing.T) {
	fake := sampleFake()
	cs := connectTest(t, newTestClient(fake, true), regConfig{})
	out := callText(t, cs, "ligolo_traceroute", tracerouteInput{IP: "10.0.0.9"})
	if fake.lastTraceIP != "10.0.0.9" {
		t.Errorf("traceroute IP not propagated: got %q", fake.lastTraceIP)
	}
	if !strings.Contains(out, "victim01") {
		t.Errorf("traceroute output unexpected: %s", out)
	}
}

func TestTracerouteRequiresIP(t *testing.T) {
	cs := connectTest(t, newTestClient(sampleFake(), true), regConfig{})
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "ligolo_traceroute", Arguments: tracerouteInput{}})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Error("expected tool error for empty IP, got success")
	}
}

func TestListCertsNeverLeaksKeys(t *testing.T) {
	cs := connectTest(t, newTestClient(sampleFake(), true), regConfig{})
	out := callText(t, cs, "ligolo_list_certs", noInput{})
	if strings.Contains(out, "SECRETKEY") || strings.Contains(out, "CERTBYTES") {
		t.Errorf("cert output leaked key/cert material: %s", out)
	}
	if !strings.Contains(out, "operator-admin") {
		t.Errorf("cert output missing name: %s", out)
	}
}

func TestAdminReadToolsGatedByPrivilege(t *testing.T) {
	adminTools := listToolNames(t, newTestClient(sampleFake(), true), regConfig{})
	for _, want := range []string{"ligolo_list_operators", "ligolo_list_certs"} {
		if !adminTools[want] {
			t.Errorf("admin read tool %q missing for admin operator", want)
		}
	}

	userTools := listToolNames(t, newTestClient(sampleFake(), false), regConfig{})
	for _, notWant := range []string{"ligolo_list_operators", "ligolo_list_certs"} {
		if userTools[notWant] {
			t.Errorf("admin read tool %q must not be registered for non-admin operator", notWant)
		}
	}
	for _, want := range []string{"ligolo_list_sessions", "ligolo_get_metadata", "ligolo_traceroute"} {
		if !userTools[want] {
			t.Errorf("read tool %q missing for non-admin operator", want)
		}
	}
}

// --- write tools (M2) ---

func TestWriteToolsGatedByFlag(t *testing.T) {
	writeToolNames := []string{
		"ligolo_rename_session", "ligolo_kill_session",
		"ligolo_start_relay", "ligolo_stop_relay",
		"ligolo_add_route", "ligolo_edit_route", "ligolo_move_route", "ligolo_del_route",
		"ligolo_add_redirector", "ligolo_del_redirector",
	}

	off := listToolNames(t, newTestClient(sampleFake(), true), regConfig{})
	for _, name := range writeToolNames {
		if off[name] {
			t.Errorf("write tool %q registered without allow-writes", name)
		}
	}

	on := listToolNames(t, newTestClient(sampleFake(), true), regConfig{allowWrites: true})
	for _, name := range writeToolNames {
		if !on[name] {
			t.Errorf("write tool %q missing with allow-writes", name)
		}
	}
	if !on["ligolo_list_sessions"] {
		t.Error("read tools should remain available with allow-writes")
	}
}

func TestWriteToolPropagatesArgs(t *testing.T) {
	fake := sampleFake()
	cs := connectTest(t, newTestClient(fake, true), regConfig{allowWrites: true})

	callText(t, cs, "ligolo_add_route", addRouteInput{SessionID: "s1", Cidr: "192.168.5.0/24", Metric: 50})
	if fake.addedRoute == nil || fake.addedRoute.GetSessionID() != "s1" {
		t.Fatalf("add_route did not propagate session id: %+v", fake.addedRoute)
	}
	if fake.addedRoute.GetRoute().GetCidr() != "192.168.5.0/24" || fake.addedRoute.GetRoute().GetMetric() != 50 {
		t.Errorf("add_route route fields wrong: %+v", fake.addedRoute.GetRoute())
	}

	callText(t, cs, "ligolo_del_redirector", delRedirectorInput{SessionID: "s1", RedirectorID: "rd9"})
	if fake.deletedRedir == nil || fake.deletedRedir.GetRedirectorID() != "rd9" {
		t.Errorf("del_redirector did not propagate id: %+v", fake.deletedRedir)
	}
}

func TestWriteToolValidatesRequiredArgs(t *testing.T) {
	cs := connectTest(t, newTestClient(sampleFake(), true), regConfig{allowWrites: true})
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ligolo_add_route",
		Arguments: addRouteInput{SessionID: "s1"}, // missing cidr
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Error("expected tool error for missing cidr, got success")
	}
}

// --- admin write tools (M3) ---

func TestAdminWriteToolsGating(t *testing.T) {
	adminWrite := []string{
		"ligolo_add_operator", "ligolo_del_operator",
		"ligolo_promote_operator", "ligolo_demote_operator",
		"ligolo_regen_cert", "ligolo_generate_agent",
	}

	// allow-admin off: absent even for an admin operator.
	off := listToolNames(t, newTestClient(sampleFake(), true), regConfig{})
	for _, name := range adminWrite {
		if off[name] {
			t.Errorf("admin write tool %q registered without allow-admin", name)
		}
	}

	// allow-admin on but operator is NOT admin: still absent.
	nonAdmin := listToolNames(t, newTestClient(sampleFake(), false), regConfig{allowAdmin: true})
	for _, name := range adminWrite {
		if nonAdmin[name] {
			t.Errorf("admin write tool %q registered for non-admin operator", name)
		}
	}

	// allow-admin on and operator is admin: present.
	on := listToolNames(t, newTestClient(sampleFake(), true), regConfig{allowAdmin: true, agentOut: t.TempDir()})
	for _, name := range adminWrite {
		if !on[name] {
			t.Errorf("admin write tool %q missing with allow-admin for admin operator", name)
		}
	}
}

func TestAddOperatorPropagatesArgs(t *testing.T) {
	fake := sampleFake()
	cs := connectTest(t, newTestClient(fake, true), regConfig{allowAdmin: true})
	callText(t, cs, "ligolo_add_operator", addOperatorInput{Name: "carol", IsAdmin: true, Server: "10.0.0.1:58008"})
	if fake.addedOperator == nil || fake.addedOperator.GetOperator().GetName() != "carol" ||
		!fake.addedOperator.GetOperator().GetIsAdmin() || fake.addedOperator.GetOperator().GetServer() != "10.0.0.1:58008" {
		t.Errorf("add_operator did not propagate args: %+v", fake.addedOperator)
	}
}

func TestGenerateAgentWritesFileNotBytes(t *testing.T) {
	fake := sampleFake()
	dir := t.TempDir()
	cs := connectTest(t, newTestClient(fake, true), regConfig{allowAdmin: true, agentOut: dir})

	out := callText(t, cs, "ligolo_generate_agent", generateAgentInput{
		GOOS: "linux", GOARCH: "amd64", Servers: []string{"1.2.3.4:11601"},
	})
	// The raw binary bytes must never appear in the tool output.
	if strings.Contains(out, "FAKEELF") {
		t.Errorf("generate_agent leaked binary bytes into output: %s", out)
	}
	var parsed generateAgentOutput
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output not JSON: %v (%s)", err, out)
	}
	if parsed.Bytes != len("FAKEELF") {
		t.Errorf("reported size wrong: %d", parsed.Bytes)
	}
	if parsed.Path == "" || !strings.HasPrefix(parsed.Path, dir) {
		t.Errorf("agent path not under agent-out dir: %q", parsed.Path)
	}
	if fake.generatedAgent.GetServers() != "1.2.3.4:11601" {
		t.Errorf("servers not propagated: %q", fake.generatedAgent.GetServers())
	}
}

func TestGenerateAgentDisabledWithoutAgentOut(t *testing.T) {
	cs := connectTest(t, newTestClient(sampleFake(), true), regConfig{allowAdmin: true}) // no agentOut
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ligolo_generate_agent",
		Arguments: generateAgentInput{GOOS: "linux", GOARCH: "amd64", Servers: []string{"1.2.3.4:11601"}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Error("expected tool error when agent-out is unset")
	}
}

// --- events (M1) ---

func TestEventBufferRingAndSnapshot(t *testing.T) {
	b := newEventBuffer(3)
	for i := 0; i < 5; i++ {
		b.add(&pb.Event{Type: 0, Data: string(rune('a' + i))})
	}
	snap := b.snapshot()
	if len(snap) != 3 {
		t.Fatalf("ring size not respected: got %d, want 3", len(snap))
	}
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

	cs := connectTest(t, client, regConfig{})
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
