package mcp

import (
	"time"

	pb "github.com/ttpreport/ligolo-mp/v2/protobuf"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The output types below shape the gRPC protobuf messages into compact,
// LLM-readable JSON. They intentionally omit binary/sensitive fields (private
// keys, raw certificate bytes, agent binaries).

type metadataOutput struct {
	Operator operatorOutput `json:"operator"`
	Server   serverOutput   `json:"server"`
}

type serverOutput struct {
	OperatorServer string `json:"operator_server"`
	AgentServer    string `json:"agent_server"`
}

type operatorOutput struct {
	Name     string `json:"name"`
	IsAdmin  bool   `json:"is_admin"`
	Server   string `json:"server,omitempty"`
	IsOnline bool   `json:"is_online,omitempty"`
}

type interfaceOutput struct {
	Name string   `json:"name"`
	IPs  []string `json:"ips,omitempty"`
}

type routeOutput struct {
	ID         string `json:"id"`
	Cidr       string `json:"cidr"`
	Metric     int32  `json:"metric"`
	IsLoopback bool   `json:"is_loopback"`
}

type redirectorOutput struct {
	ID       string `json:"id"`
	Protocol string `json:"protocol"`
	From     string `json:"from"`
	To       string `json:"to"`
}

type sessionOutput struct {
	ID          string             `json:"id"`
	Alias       string             `json:"alias,omitempty"`
	Hostname    string             `json:"hostname"`
	IsConnected bool               `json:"is_connected"`
	IsRelaying  bool               `json:"is_relaying"`
	TunName     string             `json:"tun_name,omitempty"`
	Interfaces  []interfaceOutput  `json:"interfaces,omitempty"`
	Routes      []routeOutput      `json:"routes,omitempty"`
	Redirectors []redirectorOutput `json:"redirectors,omitempty"`
	FirstSeen   string             `json:"first_seen,omitempty"`
	LastSeen    string             `json:"last_seen,omitempty"`
}

type sessionsOutput struct {
	Count    int             `json:"count"`
	Sessions []sessionOutput `json:"sessions"`
}

type traceHopOutput struct {
	IsInternal bool   `json:"is_internal"`
	Session    string `json:"session,omitempty"`
	Iface      string `json:"iface,omitempty"`
	Via        string `json:"via,omitempty"`
	Metric     int32  `json:"metric"`
}

type tracerouteOutput struct {
	Target string           `json:"target"`
	Hops   []traceHopOutput `json:"hops"`
}

type operatorsOutput struct {
	Count     int              `json:"count"`
	Operators []operatorOutput `json:"operators"`
}

// certOutput deliberately excludes the certificate and private-key bytes; only
// non-sensitive metadata is exposed through MCP.
type certOutput struct {
	Name       string `json:"name"`
	ExpiryDate string `json:"expiry_date,omitempty"`
}

type certsOutput struct {
	Count int          `json:"count"`
	Certs []certOutput `json:"certs"`
}

func fmtTime(ts *timestamppb.Timestamp) string {
	if ts == nil {
		return ""
	}
	t := ts.AsTime()
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func toMetadataOutput(resp *pb.GetMetadataResp) metadataOutput {
	var out metadataOutput
	if op := resp.GetOperator(); op != nil {
		out.Operator = operatorOutput{
			Name:     op.GetName(),
			IsAdmin:  op.GetIsAdmin(),
			Server:   op.GetServer(),
			IsOnline: op.GetIsOnline(),
		}
	}
	if cfg := resp.GetConfig(); cfg != nil {
		out.Server = serverOutput{
			OperatorServer: cfg.GetOperatorServer(),
			AgentServer:    cfg.GetAgentServer(),
		}
	}
	return out
}

func toSessionOutput(s *pb.Session) sessionOutput {
	out := sessionOutput{
		ID:          s.GetID(),
		Alias:       s.GetAlias(),
		Hostname:    s.GetHostname(),
		IsConnected: s.GetIsConnected(),
		IsRelaying:  s.GetIsRelaying(),
		FirstSeen:   fmtTime(s.GetFirstSeen()),
		LastSeen:    fmtTime(s.GetLastSeen()),
	}

	if tun := s.GetTun(); tun != nil {
		out.TunName = tun.GetName()
		for _, r := range tun.GetRoutes() {
			out.Routes = append(out.Routes, routeOutput{
				ID:         r.GetID(),
				Cidr:       r.GetCidr(),
				Metric:     r.GetMetric(),
				IsLoopback: r.GetIsLoopback(),
			})
		}
	}

	for _, iface := range s.GetInterfaces() {
		out.Interfaces = append(out.Interfaces, interfaceOutput{
			Name: iface.GetName(),
			IPs:  iface.GetIPs(),
		})
	}

	for _, rd := range s.GetRedirectors() {
		out.Redirectors = append(out.Redirectors, redirectorOutput{
			ID:       rd.GetID(),
			Protocol: rd.GetProtocol(),
			From:     rd.GetFrom(),
			To:       rd.GetTo(),
		})
	}

	return out
}

func toSessionsOutput(resp *pb.GetSessionsResp) sessionsOutput {
	sessions := resp.GetSessions()
	out := sessionsOutput{
		Count:    len(sessions),
		Sessions: make([]sessionOutput, 0, len(sessions)),
	}
	for _, s := range sessions {
		out.Sessions = append(out.Sessions, toSessionOutput(s))
	}
	return out
}

func toTracerouteOutput(target string, resp *pb.TracerouteResp) tracerouteOutput {
	out := tracerouteOutput{Target: target}
	for _, hop := range resp.GetTrace() {
		out.Hops = append(out.Hops, traceHopOutput{
			IsInternal: hop.GetIsInternal(),
			Session:    hop.GetSession(),
			Iface:      hop.GetIface(),
			Via:        hop.GetVia(),
			Metric:     hop.GetMetric(),
		})
	}
	return out
}

func toOperatorsOutput(resp *pb.GetOperatorsResp) operatorsOutput {
	ops := resp.GetOperators()
	out := operatorsOutput{
		Count:     len(ops),
		Operators: make([]operatorOutput, 0, len(ops)),
	}
	for _, op := range ops {
		out.Operators = append(out.Operators, operatorOutput{
			Name:     op.GetName(),
			IsAdmin:  op.GetIsAdmin(),
			Server:   op.GetServer(),
			IsOnline: op.GetIsOnline(),
		})
	}
	return out
}

func toCertsOutput(resp *pb.GetCertsResp) certsOutput {
	certs := resp.GetCerts()
	out := certsOutput{
		Count: len(certs),
		Certs: make([]certOutput, 0, len(certs)),
	}
	for _, cert := range certs {
		out.Certs = append(out.Certs, certOutput{
			Name:       cert.GetName(),
			ExpiryDate: cert.GetExpiryDate(),
		})
	}
	return out
}
