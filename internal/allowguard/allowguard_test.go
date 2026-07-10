package allowguard_test

import (
	"strings"
	"testing"

	"github.com/wighawag/anoncore/endpoint"
	"github.com/wighawag/anonseed/internal/allowguard"
)

// The accept/reject vectors below are BYTE-ALIGNED (same inputs accepted/rejected)
// with anonctl's internal/lanexempt test matrix: anonseed's pre-check is a
// fail-fast COPY of anonctl's authoritative policy, so it must accept and reject
// exactly what anonctl's apply-time guardrail does. If anonctl's set changes, this
// set drifts and the ADR-0002 follow-up (extract into anoncore) becomes relevant.

// TestParseAcceptsPrivateHostPort: the happy path, an exact RFC1918 host:port
// parses into a /32 host route on the exact port. This is the local-LLM case the
// exemption exists for.
func TestParseAcceptsPrivateHostPort(t *testing.T) {
	e, err := allowguard.Parse("192.168.1.150:8080")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := e.Network.String(); got != "192.168.1.150/32" {
		t.Errorf("a bare IP must normalise to a /32 host route; got %q", got)
	}
	if e.Port != 8080 {
		t.Errorf("Port = %d, want 8080", e.Port)
	}
	if !e.IsV4() {
		t.Errorf("192.168.1.150 must classify as IPv4")
	}
}

// TestParseAcceptsEveryPrivateRange: all four accepted ranges (three RFC1918
// blocks + link-local) parse, including the whole-block CIDR:port forms. Mirrors
// anonctl lanexempt's vector.
func TestParseAcceptsEveryPrivateRange(t *testing.T) {
	for _, raw := range []string{
		"10.1.2.3:22",
		"172.16.5.5:443",
		"192.168.1.150:8080",
		"169.254.1.1:80",  // link-local
		"10.0.0.0/8:8080", // whole private block, CIDR:port
		"172.16.0.0/12:443",
		"192.168.0.0/16:8080",
		"169.254.0.0/16:80",
	} {
		if _, err := allowguard.Parse(raw); err != nil {
			t.Errorf("Parse(%q) should accept a private destination, got: %v", raw, err)
		}
	}
}

// TestParseRejectsPublicLoudly: a public IP/CIDR is REJECTED loudly, naming the
// value. A public direct hole would be a real anonymity leak; the pre-check should
// catch it early instead of at anonctl apply time. Mirrors anonctl's vector.
func TestParseRejectsPublicLoudly(t *testing.T) {
	for _, raw := range []string{
		"8.8.8.8:53",
		"1.1.1.1",
		"93.184.216.34:80",
		"172.32.0.1:80", // just OUTSIDE 172.16.0.0/12
		"11.0.0.0/8",    // public
		"10.0.0.0/7",    // straddles public space (must be refused)
	} {
		_, err := allowguard.Parse(raw)
		if err == nil {
			t.Errorf("Parse(%q) must reject a public/broad destination", raw)
			continue
		}
		if !strings.Contains(err.Error(), raw) {
			t.Errorf("Parse(%q) error should name the offending value; got: %v", raw, err)
		}
	}
}

// TestParseRejectsHostnames: a hostname is REJECTED (IP/CIDR literals only). A LAN
// name cannot resolve through the forced path, and a local-resolver hole would be
// another leak. Mirrors anonctl's vector.
func TestParseRejectsHostnames(t *testing.T) {
	for _, raw := range []string{
		"my-llm.local:8080",
		"localhost:8080",
		"router:80",
	} {
		if _, err := allowguard.Parse(raw); err == nil {
			t.Errorf("Parse(%q) must reject a hostname (IP/CIDR only)", raw)
		}
	}
}

// TestParseRejectsPortOmittedLoudly: the port-omitted form (a bare IP/CIDR, no
// `:port`) is REJECTED loudly, naming the value and telling the user to add
// `:port`. A port is MANDATORY (the all-ports form is a deanonymization vector).
// Mirrors anonctl's vector.
func TestParseRejectsPortOmittedLoudly(t *testing.T) {
	for _, raw := range []string{"10.0.0.5", "192.168.0.0/24", "192.168.1.150", "169.254.1.1"} {
		_, err := allowguard.Parse(raw)
		if err == nil {
			t.Errorf("Parse(%q) must reject a port-omitted (all-ports) exemption", raw)
			continue
		}
		if !strings.Contains(err.Error(), raw) {
			t.Errorf("Parse(%q) error should name the offending value; got: %v", raw, err)
		}
		if !strings.Contains(err.Error(), ":port") {
			t.Errorf("Parse(%q) error should instruct the user to add :port; got: %v", raw, err)
		}
	}
}

// TestParseRejectsPort53Loudly: an explicit `:53` LAN exemption is REJECTED loudly
// with the DNS reason. A clear-DNS hole to a LAN resolver can reveal the local
// network's public IP. Mirrors anonctl's vector.
func TestParseRejectsPort53Loudly(t *testing.T) {
	for _, raw := range []string{
		"192.168.1.1:53",
		"10.0.0.1:53",
		"172.16.0.53:53",
		"192.168.0.0/24:53", // a whole-subnet :53 is the same clear-DNS hole
	} {
		_, err := allowguard.Parse(raw)
		if err == nil {
			t.Errorf("Parse(%q) must reject an explicit :53 exemption (a clear-DNS hole)", raw)
			continue
		}
		if !strings.Contains(err.Error(), raw) {
			t.Errorf("Parse(%q) error should name the offending value; got: %v", raw, err)
		}
		if !strings.Contains(err.Error(), "53") || !strings.Contains(strings.ToLower(err.Error()), "dns") {
			t.Errorf("Parse(%q) error should explain the DNS reason; got: %v", raw, err)
		}
	}
}

// TestParseAcceptsNonDNSPorts: the reject is scoped to 53 ONLY; a nearby port and
// DoT/853 on an exact-port LAN exemption still parse. Mirrors anonctl's vector.
func TestParseAcceptsNonDNSPorts(t *testing.T) {
	for _, raw := range []string{
		"192.168.1.1:52",
		"192.168.1.1:54",
		"192.168.1.1:853",   // DoT is encrypted DNS, not the clear-DNS leak this guards
		"192.168.0.0/24:80", // whole-subnet with an exact port
	} {
		if _, err := allowguard.Parse(raw); err != nil {
			t.Errorf("Parse(%q) should accept a non-53 exact-port exemption; got: %v", raw, err)
		}
	}
}

// TestParseAcceptsLoopbackHostPort: the loopback class, an exact 127.0.0.1:port
// parses into a /32 host route and classifies as loopback (not LAN). The same-host
// local-model case. Mirrors anonctl's vector.
func TestParseAcceptsLoopbackHostPort(t *testing.T) {
	e, err := allowguard.Parse("127.0.0.1:8080")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := e.Network.String(); got != "127.0.0.1/32" {
		t.Errorf("a bare loopback IP must normalise to a /32 host route; got %q", got)
	}
	if e.Port != 8080 {
		t.Errorf("Port = %d, want 8080", e.Port)
	}
	if !e.IsLoopback() {
		t.Errorf("127.0.0.1 must classify as loopback")
	}
	if !e.IsV4() {
		t.Errorf("127.0.0.1 is IPv4")
	}
}

// TestClassDispatchFromSameEntryPoint: the SINGLE Parse entry point dispatches on
// the typed address, loopback vs LAN, with no separate flag. Mirrors anonctl's
// vector.
func TestClassDispatchFromSameEntryPoint(t *testing.T) {
	lo, err := allowguard.Parse("127.0.0.1:8080")
	if err != nil {
		t.Fatalf("Parse(loopback): %v", err)
	}
	if !lo.IsLoopback() {
		t.Errorf("127.0.0.1:8080 must route to the loopback branch")
	}
	lan, err := allowguard.Parse("192.168.1.150:8080")
	if err != nil {
		t.Fatalf("Parse(lan): %v", err)
	}
	if lan.IsLoopback() {
		t.Errorf("192.168.1.150:8080 must route to the LAN branch, not loopback")
	}
}

// TestParseRejectsLoopbackAnonymizerPortsLoudly: the loopback guardrail is
// STRICTER than the LAN branch; a loopback exemption naming a conventional
// anonymizer control/SOCKS/DNS port ({53, 9050, 9150, 9051, 1080}) is REJECTED
// loudly, naming the port. Mirrors anonctl lanexempt's vector VERBATIM.
func TestParseRejectsLoopbackAnonymizerPortsLoudly(t *testing.T) {
	for _, tc := range []struct{ raw, portStr string }{
		{"127.0.0.1:53", "53"},     // clear DNS
		{"127.0.0.1:9050", "9050"}, // Tor SOCKS (recognised via endpoint.Classify)
		{"127.0.0.1:9150", "9150"}, // Tor Browser SOCKS (recognised via endpoint.Classify)
		{"127.0.0.1:9051", "9051"}, // Tor control (self-deanonymization vector)
		{"127.0.0.1:1080", "1080"}, // generic SOCKS
	} {
		_, err := allowguard.Parse(tc.raw)
		if err == nil {
			t.Errorf("Parse(%q) must reject a loopback anonymizer control/SOCKS/DNS port", tc.raw)
			continue
		}
		if !strings.Contains(err.Error(), tc.portStr) {
			t.Errorf("Parse(%q) error should name the offending port %s; got: %v", tc.raw, tc.portStr, err)
		}
	}
}

// TestParseAcceptsLoopbackNonAnonymizerPort: a non-anonymizer loopback port (a
// local model server) is accepted; the guardrail rejects the control surface, not
// every loopback port. Mirrors anonctl's vector.
func TestParseAcceptsLoopbackNonAnonymizerPort(t *testing.T) {
	for _, raw := range []string{
		"127.0.0.1:8080",
		"127.0.0.1:11434", // a common local-model port
		"127.0.0.1:3000",
	} {
		if _, err := allowguard.Parse(raw); err != nil {
			t.Errorf("Parse(%q) should accept a non-anonymizer loopback port; got: %v", raw, err)
		}
	}
}

// TestParseRejectsLoopbackPortOmitted: loopback has NO all-ports form; a
// port-omitted loopback value is rejected loudly, naming :port. Mirrors anonctl's
// vector.
func TestParseRejectsLoopbackPortOmitted(t *testing.T) {
	_, err := allowguard.Parse("127.0.0.1")
	if err == nil {
		t.Fatalf("Parse(127.0.0.1) must reject a port-omitted loopback exemption")
	}
	if !strings.Contains(err.Error(), ":port") {
		t.Errorf("error should instruct the user to add :port; got: %v", err)
	}
}

// TestParseRejectsLoopbackHostname: a hostname that resolves to loopback
// (localhost) is still rejected: IP/CIDR literals only, the class is decided from
// the literal, never a resolver lookup. Mirrors anonctl's vector.
func TestParseRejectsLoopbackHostname(t *testing.T) {
	if _, err := allowguard.Parse("localhost:8080"); err == nil {
		t.Errorf("Parse(localhost:8080) must reject a hostname (IP/CIDR only)")
	}
}

// TestParseRejectsMalformed: malformed / empty / bad-port values are rejected
// loudly rather than silently mis-parsed. Mirrors anonctl's vector.
func TestParseRejectsMalformed(t *testing.T) {
	for _, raw := range []string{
		"",                  // empty
		"192.168.1.150:",    // empty port
		"192.168.1.150:abc", // non-numeric port
		"192.168.1.150:0",   // out-of-range port
		"192.168.1.150:70000",
		"192.168.1.999:80", // not an IP
	} {
		if _, err := allowguard.Parse(raw); err == nil {
			t.Errorf("Parse(%q) must reject a malformed value", raw)
		}
	}
}

// TestHostPortRendersProbeTarget: an Exception renders its own exact host:port (a
// port is mandatory, so it always carries a concrete port). Mirrors anonctl's
// HostPort vector.
func TestHostPortRendersProbeTarget(t *testing.T) {
	e, err := allowguard.Parse("192.168.1.150:8080")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := e.HostPort(); got != "192.168.1.150:8080" {
		t.Errorf("HostPort = %q, want 192.168.1.150:8080 (the Exception's own port)", got)
	}
}

// TestTorPortRecognitionReusesEndpointClassify pins the REUSE: anonseed's
// loopback Tor-SOCKS rejection leans on anoncore's endpoint.Classify (9050/9150
// classify ClassTorShared), so anonseed does not re-derive the Tor-port set. If a
// future anoncore release changes which loopback ports classify tor-shared, this
// test flags the drift (the reused overlap moved).
func TestTorPortRecognitionReusesEndpointClassify(t *testing.T) {
	for _, port := range []string{"9050", "9150"} {
		raw := "socks5h://127.0.0.1:" + port
		if endpoint.Classify(raw) != endpoint.ClassTorShared {
			t.Fatalf("precondition: anoncore endpoint.Classify(%q) no longer tor-shared; allowguard's Tor-port reuse has drifted", raw)
		}
		// And the guard must reject that loopback port (via the reused classification).
		if _, err := allowguard.Parse("127.0.0.1:" + port); err == nil {
			t.Errorf("Parse(127.0.0.1:%s) must reject the Tor SOCKS port recognised via endpoint.Classify", port)
		}
	}
	// A non-Tor loopback port classifies socks-peruser AND is accepted by the guard,
	// so the reuse is scoped to the Tor subset, not every port.
	if endpoint.Classify("socks5h://127.0.0.1:11434") == endpoint.ClassTorShared {
		t.Fatalf("precondition: 11434 should not be a Tor port")
	}
	if _, err := allowguard.Parse("127.0.0.1:11434"); err != nil {
		t.Errorf("a non-Tor loopback port must be accepted; got: %v", err)
	}
}
