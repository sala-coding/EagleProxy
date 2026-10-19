package dnsx

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

// Parse reads the same addresses the settings store, and the reading decides which
// transport a resolver is reached over — getting it wrong is a resolver asked over a
// protocol it does not speak.
func TestParse(t *testing.T) {
	cases := []struct {
		in    string
		want  Transport
		host  string
		port  string
		path  string
		fails bool
	}{
		{in: "223.5.5.5", want: UDP, host: "223.5.5.5", port: "53"},
		{in: "udp://1.1.1.1", want: UDP, host: "1.1.1.1", port: "53"},
		{in: "tcp://1.1.1.1:5353", want: TCP, host: "1.1.1.1", port: "5353"},
		{in: "tls://dns.google", want: TLS, host: "dns.google", port: "853"},
		{in: "tls://[2400:3200::1]:853", want: TLS, host: "2400:3200::1", port: "853"},
		{in: "2400:3200::1", want: UDP, host: "2400:3200::1", port: "53"},
		{in: "https://dns.google/dns-query", want: HTTPS, host: "dns.google", port: "443", path: "/dns-query"},
		{in: "quic://1.1.1.1", fails: true},
		{in: "", fails: true},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if c.fails {
			if err == nil {
				t.Errorf("Parse(%q) = %+v, want an error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q) failed: %v", c.in, err)
			continue
		}
		if got.Transport != c.want || got.Host != c.host || got.Port != c.port || got.Path != c.path {
			t.Errorf("Parse(%q) = %+v, want %v %s:%s%s", c.in, got, c.want, c.host, c.port, c.path)
		}
	}
}

// A kept-open path is one connection for many questions. That is the whole reason Client
// exists — a resolver reached through the tunnel costs several round trips to dial — so it
// is worth a test rather than a comment.
func TestClientReusesOneConnection(t *testing.T) {
	addr, dials, stop := fakeTCPResolver(t)
	defer stop()

	target, err := Parse("tcp://" + addr)
	if err != nil {
		t.Fatal(err)
	}
	c := NewClient(target, Options{Timeout: time.Second})
	defer c.Close()

	for i := 0; i < 3; i++ {
		query := testQuery(uint16(100 + i))
		reply, err := c.Exchange(context.Background(), query)
		if err != nil {
			t.Fatalf("exchange %d failed: %v", i, err)
		}
		if len(reply) < 12 || binary.BigEndian.Uint16(reply[:2]) != uint16(100+i) {
			t.Fatalf("exchange %d answered with % x, want the same id back", i, reply[:min(12, len(reply))])
		}
	}
	if n := dials(); n != 1 {
		t.Errorf("dialled %d times for three questions, want 1 (the connection is kept)", n)
	}
}

// And the one-shot form is the opposite: a probe measures a handshake, so every Exchange
// opens its own connection.
func TestExchangeDialsEachTime(t *testing.T) {
	addr, dials, stop := fakeTCPResolver(t)
	defer stop()

	target, err := Parse("tcp://" + addr)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := Exchange(context.Background(), target, testQuery(uint16(200+i)), Options{Timeout: time.Second}); err != nil {
			t.Fatalf("exchange %d failed: %v", i, err)
		}
	}
	if n := dials(); n != 2 {
		t.Errorf("dialled %d times for two questions, want 2", n)
	}
}

// fakeTCPResolver is a length-prefixed DNS server that echoes the query's id back, which is
// all the transports are asked to preserve.
func fakeTCPResolver(t *testing.T) (addr string, dials func() int, stop func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var count int
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			count++
			go func(conn net.Conn) {
				defer conn.Close()
				for {
					msg, err := readTestFramed(conn)
					if err != nil {
						return
					}
					// An answer with the query's id and one empty answer record.
					reply := append([]byte(nil), msg...)
					reply[2] = 0x81
					reply[3] = 0x80
					_ = writeTestFramed(conn, reply)
				}
			}(conn)
		}
	}()
	return l.Addr().String(), func() int { return count }, func() { _ = l.Close(); <-done }
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

func readTestFramed(r io.Reader) ([]byte, error) {
	var length [2]byte
	if _, err := io.ReadFull(r, length[:]); err != nil {
		return nil, err
	}
	body := make([]byte, binary.BigEndian.Uint16(length[:]))
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

func writeTestFramed(w io.Writer, msg []byte) error {
	framed := make([]byte, 2, 2+len(msg))
	binary.BigEndian.PutUint16(framed, uint16(len(msg)))
	framed = append(framed, msg...)
	_, err := w.Write(framed)
	return err
}
