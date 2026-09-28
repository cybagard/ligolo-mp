package session

import (
	"net"
	"testing"

	"github.com/ttpreport/ligolo-mp/v2/internal/protocol"
)

func addIface(sess *Session, mac string) {
	hw, _ := net.ParseMAC(mac)
	sess.Interfaces.Append(protocol.NetInterface{
		Name:         "eth0",
		HardwareAddr: hw,
	})
}

// TestHash_AgentID_IndependentOfMACs verifies that when a stable AgentID is
// set, the identity hash ignores the interface MAC addresses. This is the
// property that lets an agent reconnect to its existing session after a VM
// snapshot revert or NIC re-provisioning changes its MACs.
func TestHash_AgentID_IndependentOfMACs(t *testing.T) {
	t.Parallel()

	a := makeSession("", "", "h")
	a.AgentID = "serial-abc"
	addIface(a, "00:11:22:33:44:55")

	b := makeSession("", "", "h")
	b.AgentID = "serial-abc"
	addIface(b, "aa:bb:cc:dd:ee:ff") // different MAC, same agent

	if a.Hash() != b.Hash() {
		t.Errorf("same AgentID with different MACs produced different hashes: %q vs %q", a.Hash(), b.Hash())
	}
}

// TestHash_DifferentAgentID_DifferentHash verifies distinct agents remain
// distinct even when they share host attributes.
func TestHash_DifferentAgentID_DifferentHash(t *testing.T) {
	t.Parallel()

	a := makeSession("", "", "h")
	a.AgentID = "serial-abc"
	addIface(a, "00:11:22:33:44:55")

	b := makeSession("", "", "h")
	b.AgentID = "serial-xyz"
	addIface(b, "00:11:22:33:44:55") // identical MAC, different agent

	if a.Hash() == b.Hash() {
		t.Error("different AgentIDs produced the same hash")
	}
}

// TestHash_NoAgentID_FallsBackToMAC verifies the legacy behavior is preserved
// when no stable AgentID is available (e.g. insecure-agent mode): identity is
// derived from interface MACs and changes when the MACs change.
func TestHash_NoAgentID_FallsBackToMAC(t *testing.T) {
	t.Parallel()

	a := makeSession("", "", "h")
	addIface(a, "00:11:22:33:44:55")

	b := makeSession("", "", "h")
	addIface(b, "aa:bb:cc:dd:ee:ff")

	if a.Hash() == b.Hash() {
		t.Error("without AgentID, different MACs should yield different hashes")
	}

	// Same MAC set → same hash (legacy reconnect path still works).
	c := makeSession("", "", "h")
	addIface(c, "00:11:22:33:44:55")
	if a.Hash() != c.Hash() {
		t.Errorf("without AgentID, identical MACs should yield identical hashes: %q vs %q", a.Hash(), c.Hash())
	}
}

// TestHash_AgentID_TakesPrecedenceOverMAC verifies the AgentID path is chosen
// over the MAC path when both are present.
func TestHash_AgentID_TakesPrecedenceOverMAC(t *testing.T) {
	t.Parallel()

	withMAC := makeSession("", "", "h")
	withMAC.AgentID = "serial-abc"
	addIface(withMAC, "00:11:22:33:44:55")

	noMAC := makeSession("", "", "h")
	noMAC.AgentID = "serial-abc"
	// no interfaces at all

	if withMAC.Hash() != noMAC.Hash() {
		t.Error("AgentID hash must not depend on interface presence")
	}
}
