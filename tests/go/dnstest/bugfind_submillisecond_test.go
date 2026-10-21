package dnstest_test

// A resolver that answers in under a millisecond is reported as having not answered
// at all. The measurement truncates to whole milliseconds (time.Since(...).Milliseconds())
// and 0 is the sentinel for "no answer came back", so a sub-millisecond answer — the
// local resolver, a resolver on the same LAN — is indistinguishable from a failure.
//
// The consequence is on the other side of the wire: the front end reads a result with
// ms <= 0 as 不可用(unavailable) and excludes it from the pick (lib/dns.ts,
// pickFastestDNS). The DNS card's whole job is to pick the fastest resolver, and the
// fastest one on the machine is the one this throws away.
//
// The fix is on the measurement side: keep a separate "did an answer come back" flag
// (or keep the time in microseconds) instead of letting 0 mean two things.

import (
	. "eagle/internal/dnstest"

	"context"
	"net"
	"testing"
	"time"
)

// startEchoResolver runs a UDP server that answers every query with the query itself,
// which is enough: checkReply only looks at the length and the id.
func startEchoResolver(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 65535)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			// Echo the query back: same id, and long enough for checkReply.
			_, _ = pc.WriteTo(buf[:n], addr)
		}
	}()
	return pc.LocalAddr().String()
}

// TestSubMillisecondAnswerIsNotAFailure is the invariant: a resolver that answered must
// never be reported with a zero latency and no error.
func TestSubMillisecondAnswerIsNotAFailure(t *testing.T) {
	addr := startEchoResolver(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Several rounds: the sub-millisecond window is a race the machine usually wins,
	// and one round losing it is not a proof. A hundred rounds makes "never 0" a
	// statement about the code rather than about the clock.
	zeros := 0
	const rounds = 100
	for i := 0; i < rounds; i++ {
		res := Run(ctx, []string{addr}, Options{Attempts: 3, Timeout: 2 * time.Second, Parallel: 1})
		if len(res) != 1 {
			t.Fatalf("round %d: got %d results, want 1", i, len(res))
		}
		if res[0].Err != "" {
			t.Fatalf("round %d: unexpected error %q", i, res[0].Err)
		}
		if res[0].Ms == 0 {
			zeros++
		}
	}
	if zeros > 0 {
		t.Fatalf("a resolver that answered was reported with 0 ms and no error in %d of %d rounds: "+
			"the measurement truncates to whole milliseconds and 0 doubles as the "+
			"\"no answer\" sentinel, so a sub-millisecond answer is read as a failure "+
			"(dnstest.measure)", zeros, rounds)
	}
}

// TestUnansweredResolverStillReportsAFailure is the other half of the same invariant,
// and the one a fix could easily break: rounding the answer up must not round the
// silence up with it. A resolver that answers nothing must still come back as an
// error and not as a reading of 1 ms.
func TestUnansweredResolverStillReportsAFailure(t *testing.T) {
	// A port nothing listens on: the dial is refused, so no answer can come back.
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := pc.LocalAddr().String()
	_ = pc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res := Run(ctx, []string{addr}, Options{Attempts: 2, Timeout: 500 * time.Millisecond, Parallel: 1})
	if len(res) != 1 {
		t.Fatalf("got %d results, want 1", len(res))
	}
	if res[0].Err == "" {
		t.Fatalf("a resolver that answered nothing was reported as a reading of %d ms with no error",
			res[0].Ms)
	}
}
