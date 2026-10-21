package dnsx

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestReviewCloseDuringDoHExchangeDereferencesNilTransport is the deterministic form
// of a race between Client.Close and Client.Exchange.
//
// dnsstub replaces a side's client whenever the resolver, the egress interface or the
// bootstrap list moves (setSide), and puts the old one away through closeClient, which
// does it on its own goroutine. Client.Close nils the DoH transport (c.http = nil) and
// drops the socket (c.conn = nil) — but Exchange never takes c.mu, and c.mu is the only
// lock in Client, so nothing serialises the two.
//
// The window is the gap inside exchangeDoHOnce between its "c.http == nil" check and
// the "c.http.Do(req)" call that follows it. A Close landing there leaves the call on a
// nil *http.Client, and http.Client.do dereferences it.
//
// The interleaving is written out here rather than raced for, because the race
// detector needs cgo and a C compiler, which this build has no. The three steps are
// the production ones: Exchange builds the transport, Close nils it, Exchange's next
// statement runs against the nil.
func TestReviewCloseDuringDoHExchangeDereferencesNilTransport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	host, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	tgt, err := Parse("https://" + net.JoinHostPort(host, port) + "/dns-query")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	c := NewClient(tgt, Options{Timeout: 3 * time.Second})
	msg := []byte{
		0x12, 0x34, // id
		0x01, 0x00, // flags
		0x00, 0x01, // qdcount
		0x00, 0x00, // ancount
		0x00, 0x00, // nscount
		0x00, 0x00, // arcount
		0x00,       // end of name
		0x00, 0x01, // type A
		0x00, 0x01, // class IN
	}

	// Step 1: an exchange runs and builds the DoH transport.
	_, _ = c.exchangeDoHOnce(context.Background(), msg)
	if c.http == nil {
		t.Fatalf("the DoH transport was not built; the premise of this check is gone")
	}

	// Step 2: the concurrent close. closeClient does exactly this, on its own
	// goroutine, while a question may still be on the client.
	_ = c.Close()
	if c.http != nil {
		t.Fatalf("Close() did not nil the DoH transport; the premise of this check is gone")
	}

	// Step 3: the statement exchangeDoHOnce runs next, verbatim.
	req, err := http.NewRequest(http.MethodPost, "https://"+tgt.Address()+tgt.Path, strings.NewReader("x"))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("exchangeDoHOnce's next statement panics on the nil transport: %v", r)
		}
	}()
	_, _ = c.http.Do(req)
}

// TestReviewExchangeAndCloseShareNoLock documents the other half: Close takes c.mu,
// Exchange does not, so the two have nothing to serialise on.
func TestReviewExchangeAndCloseShareNoLock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	host, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	tgt, err := Parse("https://" + net.JoinHostPort(host, port) + "/dns-query")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c := NewClient(tgt, Options{Timeout: 3 * time.Second})
	msg := []byte{0x12, 0x34, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 1}

	// The exchange is serialised against other exchanges by the sem channel; Close is
	// not in that scheme at all, which is the whole of the bug.
	for i := 0; i < 50; i++ {
		go func() { _, _ = c.Exchange(context.Background(), msg) }()
		go func() { _ = c.Close() }()
	}
	time.Sleep(200 * time.Millisecond)
}
