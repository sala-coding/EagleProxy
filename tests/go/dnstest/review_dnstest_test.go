package dnstest_test

import (
	"context"
	"encoding/binary"
	"net"
	"runtime"
	"testing"
	"time"

	"eagle/internal/dnstest"
)

// answerServer is a UDP resolver that answers every question with the question's
// own id and a header that claims one answer — which is all a probe asks for.
func answerServer(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	go func() {
		buf := make([]byte, 4096)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if n < 12 {
				continue
			}
			reply := make([]byte, 12)
			copy(reply, buf[:2])                          // the id the question carried
			binary.BigEndian.PutUint16(reply[2:], 0x8180) // a reply, recursion available
			binary.BigEndian.PutUint16(reply[6:], 1)      // one answer
			_, _ = pc.WriteTo(reply, addr)
		}
	}()
	return pc.LocalAddr().String()
}

// TestReviewProbeReachesALocalResolver is the plain claim: a probe asked of a
// resolver that is right there gets an answer and a time.
//
// The measurement is what the DNS card's smart pick is built on, so a probe that
// cannot reach a resolver on the same machine is a measurement that never works.
func TestReviewProbeReachesALocalResolver(t *testing.T) {
	addr := answerServer(t)
	res := dnstest.Run(context.Background(), []string{addr}, dnstest.Options{
		Timeout:  2 * time.Second,
		Attempts: 1,
	})
	if len(res) != 1 {
		t.Fatalf("Run returned %d results, want 1", len(res))
	}
	if res[0].Err != "" {
		t.Fatalf("the probe against a local resolver failed: %s", res[0].Err)
	}
	if res[0].Ms <= 0 {
		t.Errorf("the probe reported no time: %+v", res[0])
	}
}

// TestReviewProbeIsPinnedToTheInterfaceItIsGiven pins the probe to the
// interface it is handed.
//
// The domestic side of a routing profile is reached the way the forwarder
// reaches it: straight out of the machine's real interface, past the tunnel. A
// probe that took the machine's ordinary route instead would be captured by the
// tunnel, handed to the core, and answered by the core's own rules — so it would
// be measuring a path nothing uses, and failing for reasons that have nothing to
// do with the resolver.
//
// A socket bound to an interface that does not exist cannot reach anything, so
// the pin is visible from the outside: the probe fails. Windows-only, because the
// other systems have no binding to do (see internal/dnsx's bindInterface).
func TestReviewProbeIsPinnedToTheInterfaceItIsGiven(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the interface pin is a Windows thing")
	}
	addr := answerServer(t)
	res := dnstest.Run(context.Background(), []string{addr}, dnstest.Options{
		Timeout:        2 * time.Second,
		Attempts:       1,
		InterfaceIndex: 9999, // no interface has this index
	})
	if res[0].Err == "" {
		t.Fatal("the probe ignored the interface pin: a socket bound to an interface that does not exist still reached the resolver")
	}
}
