package dnsstub

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

// startFor brings a forwarder up for one test: on ports the system picks (the app itself
// may be running and holding the production ones, which is a fact about the machine rather
// than a fault), with the test as its log sink. The reasons a side is not served — a
// listener that would not bind, an upstream that cannot be reached the way that side has to
// be — are logged rather than returned, and a test that cannot see them is a test that fails
// without saying why.
func startFor(t *testing.T) *Server {
	t.Helper()
	return startOn(0, 0, func(line string) { t.Log(line) })
}

// The fallback is the property that matters most here: a helper that can take the machine's
// resolution down with it would be worse than the button it exists for. No upstream means
// the core is told nothing (that side is out of Endpoints) and a query that arrives anyway
// is answered SERVFAIL rather than dropped.
func TestNoUpstreamFallsBack(t *testing.T) {
	s := startFor(t)
	defer Stop()
	remote, direct := s.Endpoints()
	if remote != "" || direct != "" {
		t.Errorf("Endpoints = %q / %q with no upstream, want both empty", remote, direct)
	}
	reply := roundTrip(t, s.addr(Remote), testQuery(7))
	if rcode(reply) != 2 {
		t.Errorf("rcode = %d, want 2 (SERVFAIL)", rcode(reply))
	}
	if got := binary.BigEndian.Uint16(reply[:2]); got != 7 {
		t.Errorf("answer id = %d, want the query's 7", got)
	}
}

// The foreign side may not be asked in clear: its queries are handed to the core as a
// stream, and a plain resolver reached any other way is a query that left the tunnel.
func TestRemoteRefusesPlain(t *testing.T) {
	s := startFor(t)
	defer Stop()
	s.Configure(Wires{Remote: "119.29.29.29", Tunnel: true, Egress: 9})
	if got, _ := s.Endpoints(); got != "" {
		t.Errorf("remote endpoint = %q for a plain resolver, want empty", got)
	}
	if up, _ := s.Upstreams(); up != "" {
		t.Errorf("remote upstream = %q, want empty", up)
	}
}

// The domestic side may be asked in clear, but only when the query can be kept out of the
// tunnel: pinned to a real interface, or with no tunnel up at all. Without either, the
// query would fall into the tunnel, the core's hijack would answer it, and the answer would
// be asked of this forwarder again.
func TestDirectPlainNeedsAWayOut(t *testing.T) {
	s := startFor(t)
	defer Stop()

	s.Configure(Wires{Direct: "223.5.5.5", Tunnel: true, Egress: 0})
	if _, got := s.Endpoints(); got != "" {
		t.Errorf("direct endpoint = %q with a tunnel and no egress, want empty", got)
	}

	s.Configure(Wires{Direct: "223.5.5.5", Tunnel: true, Egress: 9})
	if _, got := s.Endpoints(); got == "" {
		t.Error("direct endpoint is empty with an egress interface to pin the dial to")
	}

	s.Configure(Wires{Direct: "223.5.5.5", Tunnel: false, Egress: 0})
	if _, got := s.Endpoints(); got == "" {
		t.Error("direct endpoint is empty with no tunnel up, where clear DNS is reached the ordinary way")
	}
}

// Both sides are served at once, and switching either one does not touch a listener: that
// is the point of the package — the core's config keeps naming these two ports.
func TestBothSidesSwitchWithoutRestart(t *testing.T) {
	s := startFor(t)
	defer Stop()
	s.Configure(Wires{Remote: "tls://dns.google", Direct: "https://doh.pub/dns-query"})
	remote, direct := s.Endpoints()
	if remote == "" || direct == "" {
		t.Fatalf("Endpoints = %q / %q, want both served", remote, direct)
	}
	if remote == direct {
		t.Fatalf("both sides answer on %q; they must be told apart", remote)
	}
	s.Configure(Wires{Remote: "https://dns.google/dns-query", Direct: "tls://223.5.5.5"})
	if got, got2 := s.Endpoints(); got != remote || got2 != direct {
		t.Errorf("listeners moved to %q / %q; the core asks the same ports", got, got2)
	}
	if up, down := s.Upstreams(); up != "https://dns.google/dns-query" || down != "tls://223.5.5.5" {
		t.Errorf("upstreams = %q / %q, want the new pair", up, down)
	}
}

// The two hand-rolled pieces of DNS grammar: what counts as a query worth forwarding, and
// where a question ends (which is what a SERVFAIL echoes).
func TestQueryShapes(t *testing.T) {
	q := testQuery(1)
	if !isQuery(q) {
		t.Error("a single-question query was not recognised")
	}
	response := append([]byte(nil), q...)
	response[2] |= 0x80
	if isQuery(response) {
		t.Error("a response was taken for a query")
	}
	if isQuery(q[:8]) {
		t.Error("a short message was taken for a query")
	}
	if end := questionEnd(q); end != len(q) {
		t.Errorf("questionEnd = %d, want %d", end, len(q))
	}
	// A SERVFAIL echoes the header and the question, and nothing else.
	sf := servfail(q)
	if len(sf) != len(q) {
		t.Errorf("servfail length = %d, want the query's %d", len(sf), len(q))
	}
	if sf[2]&0x80 == 0 {
		t.Error("servfail is not marked as a response")
	}
	if binary.BigEndian.Uint16(sf[6:8]) != 0 {
		t.Error("servfail carries an answer count")
	}
}

// roundTrip sends one query to one side of the forwarder over the loopback, the way the
// core does.
func roundTrip(t *testing.T, addr string, query []byte) []byte {
	t.Helper()
	if addr == "" {
		t.Fatal("that side has no listener to ask")
	}
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if err := writeFramed(conn, query); err != nil {
		t.Fatalf("write: %v", err)
	}
	reply, err := readFramed(io.Reader(conn))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return reply
}

func rcode(msg []byte) int {
	if len(msg) < 4 {
		return -1
	}
	return int(msg[3] & 0x0F)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// testQuery is a minimal A query for www.example.com with the given id.
func testQuery(id uint16) []byte {
	b := []byte{0, 0, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint16(b[:2], id)
	for _, label := range []string{"www", "example", "com"} {
		b = append(b, byte(len(label)))
		b = append(b, label...)
	}
	b = append(b, 0, 0, 1, 0, 1)
	return b
}
