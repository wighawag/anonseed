// Package allowguard is anonseed's FAIL-FAST pre-validation for a direct-egress
// `--allow` value: the parse+validate that turns an operator-supplied
// `IP|CIDR:port` into a validated Exception (the LAN/loopback hole a seeded tool
// needs). It is all pure logic (no root, no sockets, no system mutation), so the
// accept/reject matrix is exhaustively unit-testable everywhere (the default
// `go test ./...`).
//
// # This is NOT the security boundary (the load-bearing layering)
//
// anonctl's own `internal/lanexempt` is the AUTHORITATIVE guardrail. anonctl
// RE-VALIDATES every default through it on every `add`/`update` (README: "a
// default exemption is validated through the same guardrail as the flag"), so a
// bad value anonseed somehow wrote into `/etc/anonctl/defaults.json` is STILL
// rejected by anonctl at apply time. This package exists ONLY so anonseed fails
// EARLY, with a clear message, instead of letting a bad value sit until the next
// `add` fails. It is CONVENIENCE (better UX), not the boundary.
//
// anonctl's `lanexempt` is Go-INTERNAL (un-importable), and there is no anonctl
// pure-validate CLI verb to shell out to (`anonctl verify` is a LIVE egress
// prover needing root + a provisioned account, the WRONG tool, and spec story 26
// forbids anonseed re-implementing an egress prover). So anonseed keeps a small
// ALIGNED COPY of the allow-list POLICY here. The drift risk this creates, and
// the follow-up to extract the guardrail into anoncore if a third consumer
// appears, are recorded in docs/adr/0002 and work/notes/ideas/.
//
// # What is reused vs anonseed-local
//
// The ADDRESS primitives lean on anoncore's endpoint package where they genuinely
// overlap: the loopback host constant is endpoint.DefaultHost, and the
// Tor-SOCKS-port recognition (9050/9150) is endpoint.Classify. Only the allow-list
// POLICY is anonseed-local, mirroring anonctl's lanexempt VERBATIM on the
// accept/reject matrix: RFC1918 + link-local containment (LAN branch), the
// STRICTER loopback branch's full control/SOCKS/DNS port blocklist
// ({53, 9050, 9150, 9051, 1080}), the `:53`-on-LAN refusal, and the MANDATORY
// port (no all-ports form; that form is a deanonymization vector when the
// exempted host runs a forwarding proxy on some other port).
//
// # Dispatch
//
// Like lanexempt, Parse DISPATCHES on the address class the operator typed
// (loopback 127.0.0.0/8 or ::1 vs RFC1918/link-local LAN): the user made the
// class obvious by typing 127.0.0.1 vs 192.168.x.x, so no separate flag is
// needed. A hostname or any non-IP/CIDR literal is refused (a LAN name cannot
// resolve through the forced path, and a local-resolver hole would be another
// leak).
package allowguard

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/wighawag/anoncore/endpoint"
)

// Exception is one VALIDATED direct-egress `--allow` hole: a private-LAN or
// loopback `IP|CIDR:port` an anonymized seed may reach DIRECTLY (over the real
// NIC, or on loopback for a same-host service) while all other egress stays
// forced through the proxy. It is the parse+validate output of this package,
// mirroring anonctl's lanexempt.Exempt.
//
// This is the VALIDATED form of the string a seed.Exception carries in its Allow
// field: seed.Exception (in internal/seed) is the DECLARATIVE plan carrier (a raw
// string + reason, JSON-serialised into a SeedPlan), whereas THIS Exception is
// the parsed, guardrail-checked value. A seed's applier validates the raw
// seed.Exception.Allow through Parse here and lands the value on success; the two
// share anonseed's ubiquitous word "Exception" deliberately (CONTEXT.md), split
// only by layer (declarative carrier vs validated value). See docs/adr/0002.
//
// Network is always non-nil on a value that survived Parse. A bare IP is
// normalised to a host route (/32 for IPv4, /128 for IPv6). Port is the exact TCP
// destination port and is always > 0: a port is mandatory (Parse rejects a
// port-omitted value), so an Exception always names EXACTLY one service.
type Exception struct {
	Network *net.IPNet // the exempted destination network (a /32 or /128 for a bare IP)
	Port    int        // exact TCP port (always > 0; a port is mandatory)
	Raw     string     // the original value, preserved for diagnostics
}

// dnsPort is the clear-DNS port (53). It is UN-EXEMPTABLE: Parse rejects an
// explicit `:53` exemption (on both classes), so a hole can never carry clear
// DNS. It matches anonctl lanexempt.DNSPort.
const dnsPort = 53

// HostPort renders the Exception as a dialable `host:port` (the network's base
// address plus the exact TCP port, always present). It never renders 53
// (un-exemptable). Mirrors lanexempt.Exempt.HostPort.
func (e Exception) HostPort() string {
	if e.Network == nil {
		return ""
	}
	return net.JoinHostPort(e.Network.IP.String(), strconv.Itoa(e.Port))
}

// IsV4 reports whether the Exception is an IPv4 destination. Mirrors
// lanexempt.Exempt.IsV4.
func (e Exception) IsV4() bool { return e.Network != nil && e.Network.IP.To4() != nil }

// IsLoopback reports whether the Exception is a LOOPBACK-class destination
// (127.0.0.0/8 for v4, ::1 for v6), as opposed to an RFC1918/link-local LAN
// destination. This is the class Parse DISPATCHES on. Mirrors
// lanexempt.Exempt.IsLoopback.
func (e Exception) IsLoopback() bool {
	return e.Network != nil && e.Network.IP.IsLoopback()
}

// privateRanges is the set of RFC1918 / link-local destination ranges the LAN
// branch accepts. The loopback branch accepts 127.0.0.0/8 (and ::1) instead,
// guarded by its own STRICTER port blocklist (loopbackAnonymizerPorts).
// Restricting LAN exemptions to these ranges is the security policy: a value not
// FULLY contained in one of these (public, or a too-wide prefix that straddles
// public space) is refused loudly. Mirrors anonctl lanexempt.privateRanges
// VERBATIM.
var privateRanges = func() []*net.IPNet {
	cidrs := []string{
		"10.0.0.0/8",     // RFC1918
		"172.16.0.0/12",  // RFC1918
		"192.168.0.0/16", // RFC1918
		"169.254.0.0/16", // link-local (RFC3927)
	}
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic("allowguard: bad built-in private range " + c) // unreachable: constants
		}
		nets = append(nets, n)
	}
	return nets
}()

// loopbackAnonymizerPorts is the loopback branch's STRICTER port blocklist: the
// conventional, host-independent anonymizer control/SOCKS/DNS ports a loopback
// exemption must NEVER name, because loopback is the anonymizer's OWN control
// surface. It mirrors anonctl lanexempt.loopbackAnonymizerPorts VERBATIM
// ({53, 9050, 9150, 9051, 1080}). The value is the human-readable reason, named
// in the reject.
//
// Note the OVERLAP with anoncore endpoint: endpoint.Classify recognises the
// Tor-SOCKS subset (9050/9150) as ClassTorShared. isAnonTorPort reuses
// endpoint.Classify for exactly that subset (rather than re-spelling 9050/9150),
// so anonseed's Tor-port recognition cannot drift from anoncore's; the BROADER
// set (53 clear-DNS, 9051 Tor CONTROL, 1080 generic SOCKS) is anonseed/anonctl
// POLICY that endpoint does not model, so it is enumerated here with its reason.
var loopbackAnonymizerPorts = map[int]string{
	53:   "clear DNS (must go through the anonymizer, never a direct query)",
	9051: "the conventional Tor CONTROL port (reachable from the account it is a self-deanonymization vector)",
	1080: "the conventional generic SOCKS port (dialling it directly would skip the forced path)",
}

// torSocksBlockReason is the shared reason for a loopback Tor-SOCKS port
// (9050/9150), which isAnonTorPort recognises via anoncore's endpoint.Classify
// rather than a re-spelled port literal.
const torSocksBlockReason = "the conventional Tor / Tor Browser SOCKS port (dialling it directly would skip the forced path and its <account>@ isolation)"

// isAnonTorPort reports whether a loopback port is a conventional Tor SOCKS port
// (9050/9150), REUSING anoncore's endpoint.Classify: a loopback socks5h endpoint
// on such a port classifies ClassTorShared. This is the genuine overlap the task
// calls for, so anonseed does not re-derive the Tor-port set anoncore already
// exposes; the broader anonseed-local policy ports live in loopbackAnonymizerPorts.
func isAnonTorPort(port int) bool {
	raw := "socks5h://" + net.JoinHostPort(loopbackHost, strconv.Itoa(port))
	return endpoint.Classify(raw) == endpoint.ClassTorShared
}

// Parse is the FAIL-FAST pre-validation: it parses one `--allow` value into a
// validated Exception and DISPATCHES on the address class the operator typed. A
// loopback literal (127.0.0.0/8, ::1) routes to the STRICTER loopback guardrail;
// an RFC1918/link-local literal routes to the LAN guardrail. Both require a
// MANDATORY `:port` and reject a hostname, a malformed value, and an
// out-of-range/non-numeric port. The LAN branch rejects anything not fully within
// the private/link-local ranges and rejects an explicit `:53`; the loopback
// branch additionally rejects the well-known anonymizer control/SOCKS/DNS ports.
//
// It mirrors anonctl lanexempt.Parse VERBATIM on the accept/reject matrix. It is
// NOT the security boundary: anonctl re-validates through its own lanexempt at
// apply time (see the package doc). This is the early, clearer failure.
func Parse(raw string) (Exception, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return Exception{}, fmt.Errorf("empty --allow value: expected an RFC1918/link-local or 127.0.0.1 IP or CIDR, with a mandatory :port")
	}

	hostPart, port, err := splitPort(value)
	if err != nil {
		return Exception{}, err
	}

	// Reject a port-omitted value: a port is MANDATORY for BOTH classes. The
	// all-ports form (a bare IP/CIDR, no `:port`) opened every TCP port except 53,
	// a deanonymization leak if the exempted host runs a forwarding proxy on some
	// other port (ssh -D SOCKS, squid, a Tor SocksPort, a socat tunnel): the anon
	// account could dial that proxy directly and egress the whole internet from the
	// real IP. The only defensible granularity is "reach exactly this service".
	if port == 0 {
		return Exception{}, fmt.Errorf(
			"--allow %q has no port: a port is mandatory (an all-ports hole to a host running a forwarding proxy would leak your real IP); add :port for the exact service, e.g. %s:8080",
			raw, hostPart)
	}

	network, err := parseHostToNetwork(hostPart, value)
	if err != nil {
		return Exception{}, err
	}

	e := Exception{Network: network, Port: port, Raw: raw}

	// Class-dispatch on the address the user typed. A loopback literal is the
	// anonymizer's OWN control surface, so it goes through the stricter loopback
	// guardrail; everything else must be a private/link-local LAN destination.
	if e.IsLoopback() {
		// A Tor SOCKS port (9050/9150) is recognised via anoncore's endpoint.Classify
		// (the reused overlap); the remaining control/SOCKS/DNS ports are the
		// anonseed-local policy set. Either is refused loudly, naming the port.
		if isAnonTorPort(port) {
			return Exception{}, fmt.Errorf(
				"--allow %q targets loopback port %d: %s; loopback is the anonymizer's own control surface, so port %d cannot be exempted",
				raw, port, torSocksBlockReason, port)
		}
		if reason, blocked := loopbackAnonymizerPorts[port]; blocked {
			return Exception{}, fmt.Errorf(
				"--allow %q targets loopback port %d: %s; loopback is the anonymizer's own control surface, so port %d cannot be exempted",
				raw, port, reason, port)
		}
		return e, nil
	}

	// Reject an explicit clear-DNS port (53) on a LAN destination. A LAN DNS hole
	// (`@192.168.x.x`) can reveal the local network's public IP (a deanonymization
	// vector), so 53 is UN-EXEMPTABLE: DNS must go through the anonymizer.
	// (Loopback 53 is already rejected above by loopbackAnonymizerPorts.)
	if port == dnsPort {
		return Exception{}, fmt.Errorf(
			"--allow %q targets DNS port 53: a direct clear-DNS query to a LAN resolver can reveal your local network's public IP (a deanonymization vector); DNS must go through the anonymizer, so port 53 cannot be exempted",
			raw)
	}

	if !networkWithinPrivateRanges(network) {
		return Exception{}, fmt.Errorf(
			"--allow %q is not a private or loopback address: only RFC1918 / link-local ranges (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 169.254.0.0/16) or loopback (127.0.0.1) may be exempted for direct egress; a public destination would leak your real IP around the forced path",
			raw)
	}

	return e, nil
}

// loopbackHost is the loopback host constant, reused from anoncore's endpoint
// package (endpoint.DefaultHost == "127.0.0.1") rather than re-spelled, so the
// one place anonseed and anonctl name the loopback host cannot drift.
const loopbackHost = endpoint.DefaultHost

// splitPort separates an optional trailing `:port` from the host (IP or CIDR)
// part, disambiguating the `:` before a port from the `:` inside an IPv6 literal.
// A present-but-invalid port is rejected here (naming the value). Mirrors anonctl
// lanexempt.splitPort.
func splitPort(value string) (host string, port int, err error) {
	idx := strings.LastIndexByte(value, ':')
	if idx < 0 {
		return value, 0, nil // no port
	}

	// An unbracketed multi-colon token is a possible IPv6 literal with no port,
	// not a host:port; let network parsing decide. A bracketed IPv6 with a port
	// ("[fe80::1]:80") is out of scope for v1, so it is left to fail network
	// parsing.
	if strings.Count(value, ":") > 1 && !strings.Contains(value, "]") {
		return value, 0, nil
	}

	host = value[:idx]
	portStr := value[idx+1:]
	if portStr == "" {
		return "", 0, fmt.Errorf("--allow %q has an empty port after ':': expected :<1-65535>", value)
	}
	p, perr := strconv.Atoi(portStr)
	if perr != nil {
		return "", 0, fmt.Errorf("--allow %q has a non-numeric port %q: expected :<1-65535>", value, portStr)
	}
	if p < 1 || p > 65535 {
		return "", 0, fmt.Errorf("--allow %q has an out-of-range port %d: expected :<1-65535>", value, p)
	}
	return host, p, nil
}

// parseHostToNetwork turns the host part (an IP or a CIDR) into a normalised
// *net.IPNet: a bare IP becomes a host route (/32 for IPv4, /128 for IPv6). A
// value that is neither a valid IP nor a valid CIDR literal (e.g. a hostname) is
// rejected, naming the original value. Mirrors anonctl lanexempt.parseHostToNetwork.
func parseHostToNetwork(host, value string) (*net.IPNet, error) {
	if strings.Contains(host, "/") {
		_, network, err := net.ParseCIDR(host)
		if err != nil {
			return nil, fmt.Errorf("--allow %q is not a valid CIDR: %v (IP/CIDR literals only; hostnames are unsupported)", value, err)
		}
		return network, nil
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return nil, fmt.Errorf("--allow %q is not a valid IP or CIDR literal (hostnames are unsupported: a LAN or loopback name cannot resolve through the forced path)", value)
	}
	bits := 32
	if ip.To4() == nil {
		bits = 128
	}
	return &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)}, nil
}

// networkWithinPrivateRanges reports whether the whole network is contained in
// one of the accepted private/link-local ranges (both endpoints inside the same
// range, so a too-wide prefix that straddles public space is refused). Mirrors
// anonctl lanexempt.networkWithinPrivateRanges.
func networkWithinPrivateRanges(n *net.IPNet) bool {
	for _, r := range privateRanges {
		if rangeContainsNetwork(r, n) {
			return true
		}
	}
	return false
}

// rangeContainsNetwork reports whether accepted range r fully contains network n
// (both endpoints of n lie in r). Mirrors anonctl lanexempt.rangeContainsNetwork.
func rangeContainsNetwork(r, n *net.IPNet) bool {
	first := n.IP
	last := lastAddr(n)
	if first == nil || last == nil {
		return false
	}
	return r.Contains(first) && r.Contains(last)
}

// lastAddr returns the last address of a network (network address OR'd with the
// inverted mask). Mirrors anonctl lanexempt.lastAddr.
func lastAddr(n *net.IPNet) net.IP {
	ip := n.IP
	mask := n.Mask
	if len(ip) != len(mask) {
		if v4 := ip.To4(); v4 != nil && len(mask) == net.IPv4len {
			ip = v4
		} else {
			return nil
		}
	}
	last := make(net.IP, len(ip))
	for i := range ip {
		last[i] = ip[i] | ^mask[i]
	}
	return last
}
