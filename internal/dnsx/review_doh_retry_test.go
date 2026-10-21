package dnsx

import (
	"context"
	"net"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

// TestReviewDoHAsksAgainAfterAFailedConnection covers the DoH path's answer to
// a connection that died without saying so.
//
// A kept-open connection can be dead without having said so: the resolver closed
// it while it was idle, the tunnel was rebuilt under it. The stream path drops
// the socket and asks once more; the DoH path has to throw the pooled connection
// away to do the same, because the request it already wrote cannot be replayed
// by the transport.
//
// Without the second attempt, one dead connection costs the resolver every
// question until the transport happens to notice — which is what made the
// foreign side fail over and over while the resolver itself was fine.
func TestReviewDoHAsksAgainAfterAFailedConnection(t *testing.T) {
	// A proxy that takes the connection and hangs up: every attempt through it
	// fails, and every attempt is counted.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	var attempts int32
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			atomic.AddInt32(&attempts, 1)
			_ = conn.Close()
		}
	}()

	target, err := Parse("https://doh.example.test/dns-query")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	c := NewClient(target, Options{
		Proxy:   &url.URL{Scheme: "http", Host: ln.Addr().String()},
		Timeout: 2 * time.Second,
	})
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := c.Exchange(ctx, []byte{0x12, 0x34, 0x01, 0x00}); err == nil {
		t.Fatal("the exchange succeeded against a proxy that hangs up")
	}

	if got := atomic.LoadInt32(&attempts); got < 2 {
		t.Errorf("the DoH path tried %d time(s): a connection that died without saying so is not retried, so the next question meets the same dead connection", got)
	}
}

// TestReviewDoHBuildsAFreshTransportAfterAFailure is the other half: the retry
// is not asked on the same pooled connection, but on a transport that has never
// seen the dead one.
func TestReviewDoHBuildsAFreshTransportAfterAFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	target, err := Parse("https://doh.example.test/dns-query")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	c := NewClient(target, Options{
		Proxy:   &url.URL{Scheme: "http", Host: ln.Addr().String()},
		Timeout: 500 * time.Millisecond,
	})
	defer c.Close()

	before := c.dohTransport()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = c.Exchange(ctx, []byte{0x12, 0x34, 0x01, 0x00})

	after := c.dohTransport()
	if before == after {
		t.Error("the transport was not thrown away after the failure: the next question meets the same pooled connection")
	}
}
