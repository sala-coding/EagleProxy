package dnsstub

import (
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"eagle/internal/dnstest"
)

// failingUpstream is a resolver that takes the question and hangs up on it.
//
// Every question it is asked fails, and it remembers which questions it was
// asked — by their id, because a question asked twice (the client's own retry
// after a connection that died) is still one question.
func failingUpstream(t *testing.T) (addr string, asked func() int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	var mu sync.Mutex
	seen := map[uint16]bool{}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				// A question over a stream carries its length first (see the
				// dnsx client's stream exchange), and the id is inside it.
				var lenBuf [2]byte
				if _, err := io.ReadFull(c, lenBuf[:]); err != nil {
					return
				}
				body := make([]byte, int(binary.BigEndian.Uint16(lenBuf[:])))
				if _, err := io.ReadFull(c, body); err != nil {
					return
				}
				if len(body) >= 2 {
					mu.Lock()
					seen[binary.BigEndian.Uint16(body[:2])] = true
					mu.Unlock()
				}
				// Hang up on it: the question is never answered, and the client
				// finds out at once instead of waiting out its own timeout.
			}(conn)
		}
	}()
	return ln.Addr().String(), func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(seen)
	}
}

// ask sends one question to the forwarder's side and reads its answer back.
func ask(t *testing.T, s *Server, which Side, id uint16) []byte {
	t.Helper()
	addr := s.addr(which)
	if addr == "" {
		t.Fatalf("the %s side has no address", which)
	}
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err := writeFramed(conn, dnstest.Query(id)); err != nil {
		t.Fatalf("write: %v", err)
	}
	reply, err := readFramed(conn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return reply
}

// isServfail says whether the answer is the one the forwarder gives when it has
// no answer to give.
func isServfail(reply []byte) bool {
	if len(reply) < 12 {
		return false
	}
	return binary.BigEndian.Uint16(reply[2:4])&0x0F == 2
}

// TestReviewAFailingSideIsNotAskedAgainAtOnce is the whole of the fast answer.
//
// A side whose upstream is not answering used to be asked anyway, and every
// question waited out the full timeout before it was told "no". A machine
// loading a page asks a dozen names at once, so that is a dozen waits of five
// seconds each — the difference between a browser that says it failed and one
// that sits there.
//
// So: after the upstream has failed twice in a row, the side stops asking it,
// and the questions are answered at once. One question in failRetryAfter is still
// asked, so a side that comes back is noticed.
func TestReviewAFailingSideIsNotAskedAgainAtOnce(t *testing.T) {
	addr, asked := failingUpstream(t)

	s := startOn(0, 0, nil)
	defer s.Close()
	s.setSide(Direct, "tcp://"+addr, Wires{})

	// The first two questions are asked: one failure is not a pattern.
	for i := uint16(1); i <= 2; i++ {
		reply := ask(t, s, Direct, i)
		if !isServfail(reply) {
			t.Fatalf("question %d was answered with something other than a SERVFAIL: %v", i, reply)
		}
	}
	if got := asked(); got != 2 {
		t.Fatalf("the upstream was asked %d time(s), want 2: the side gave up before it had a pattern", got)
	}

	// The third one is not asked at all.
	reply := ask(t, s, Direct, 3)
	if !isServfail(reply) {
		t.Fatalf("question 3 was answered with something other than a SERVFAIL: %v", reply)
	}
	if got := asked(); got != 2 {
		t.Errorf("the upstream was asked %d time(s) after the side had failed twice, want 2: every question is still paying the full timeout", got)
	}
}

// TestReviewAFailingSideTriesAgainAfterAWhile is the other half: the side is not
// given up on. One question in failRetryAfter is asked, and an answer from it
// puts the side back in service.
func TestReviewAFailingSideTriesAgainAfterAWhile(t *testing.T) {
	old := failRetryAfter
	failRetryAfter = 50 * time.Millisecond
	defer func() { failRetryAfter = old }()

	addr, asked := failingUpstream(t)

	s := startOn(0, 0, nil)
	defer s.Close()
	s.setSide(Direct, "tcp://"+addr, Wires{})

	for i := uint16(1); i <= 2; i++ {
		ask(t, s, Direct, i)
	}
	if got := asked(); got != 2 {
		t.Fatalf("the upstream was asked %d time(s), want 2", got)
	}

	// Right away: still not asked.
	ask(t, s, Direct, 3)
	if got := asked(); got != 2 {
		t.Errorf("the upstream was asked %d time(s) before the window was up, want 2", got)
	}

	// And after the window: asked once more, because a side that has come back
	// has to be noticed.
	time.Sleep(failRetryAfter + 20*time.Millisecond)
	ask(t, s, Direct, 4)
	if got := asked(); got != 3 {
		t.Errorf("the upstream was asked %d time(s) after the window was up, want 3: a side that comes back is never noticed", got)
	}
}
