package dnsstub

import (
	"encoding/binary"
	"net"
	"testing"
	"time"
)

// The core is told to ask the forwarder over TCP (see configbuilder.stubServer)
// and it may keep that connection open and reuse it. So the connection has to
// stay usable for as long as it is in use.
//
// It did not. The deadline was set once, when the connection was accepted, and
// a deadline set once is absolute: it does not move along with the traffic. The
// connection was cut two minutes after it was made — however busy it was — and
// the answer to the question that was on it could not even be sent back, since
// the write deadline had passed as well. The queries on it failed every two
// minutes for the life of the tunnel.
//
// The allowance is shortened here so the test does not have to wait two
// minutes; what it pins is the shape of the fix — a deadline that is refreshed
// per question, not one that is set once and expires whatever the traffic is.
func TestBusyForwardedConnectionIsNotCut(t *testing.T) {
	old := serveTimeout
	serveTimeout = 300 * time.Millisecond
	defer func() { serveTimeout = old }()

	s := startFor(t)
	defer Stop()

	conn, err := net.DialTimeout("tcp", s.addr(Direct), 2*time.Second)
	if err != nil {
		t.Fatalf("dial the forwarder: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	// One question every 100ms for two seconds: ten times the allowance, so the
	// connection is busy the whole time. A deadline set once at accept would
	// have expired in the middle of this and cut the connection under a
	// question that was on it.
	for i := 1; i <= 20; i++ {
		if err := writeFramed(conn, testQuery(uint16(i))); err != nil {
			t.Fatalf("the connection was cut while it was busy, at question %d: %v", i, err)
		}
		reply, err := readFramed(conn)
		if err != nil {
			t.Fatalf("the connection was cut while it was busy, at question %d: %v", i, err)
		}
		if got := binary.BigEndian.Uint16(reply[:2]); got != uint16(i) {
			t.Fatalf("answer id = %d, want the query's %d", got, i)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
