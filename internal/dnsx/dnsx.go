// Package dnsx carries one DNS message to one resolver and brings the answer back.
//
// Two callers, one implementation. The DNS card's probe (internal/dnstest) measures how
// long a resolver takes to answer; the app's own forwarder (internal/dnsstub) relays
// whatever the core asks. Both need the same transports and the same way of reaching a
// resolver through the running core, and a second copy of that would be a second place
// for the framing, the proxy hop and the truncation rules to be got wrong.
//
// The address syntax is the settings' own, because the reader is who types it: a bare
// address is plain DNS over UDP, and udp:// / tcp:// / tls:// / https:// say what a
// written one means. It is the same reading the config builder gives the same string
// (see configbuilder.splitDNSAddress) — one string, and the core and this package agree
// about what it says.
package dnsx

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Transport is how a resolver is spoken to.
type Transport int

const (
	// UDP is one datagram each way, which is what a bare address means.
	UDP Transport = iota
	// TCP is the same message length-prefixed on a stream.
	TCP
	// TLS is that stream wrapped in TLS (DNS over TLS, port 853).
	TLS
	// HTTPS is the message POSTed to an endpoint (DNS over HTTPS, port 443).
	HTTPS
)

// target is an address taken apart.
type target struct {
	Transport Transport
	Host      string
	Port      string
	// Path is the DoH endpoint's, which is not always /dns-query.
	Path string
}

// Target is an address a caller may hand to several Exchanges. It is the parsed form of
// a resolver address; build one with Parse.
type Target = target

// Parse reads an address the way the config builder does: a scheme, a host, and a port
// that the scheme fills in when it is not written. A port may only be read off a colon
// that is not part of an IPv6 address, which is why the brackets matter.
func Parse(addr string) (Target, error) {
	rest := strings.TrimSpace(addr)
	t := target{Transport: UDP, Port: "53"}
	if scheme, after, ok := strings.Cut(rest, "://"); ok {
		rest = after
		switch scheme {
		case "https":
			t.Transport, t.Port = HTTPS, "443"
			// Only DoH has an endpoint to name; a path on any other transport would be a
			// field nothing reads, which is the kind of thing that gets believed later.
			t.Path = "/dns-query"
		case "tls":
			t.Transport, t.Port = TLS, "853"
		case "tcp":
			t.Transport = TCP
		case "udp":
			// a plain resolver written with its scheme: still a plain resolver
		default:
			return t, fmt.Errorf("不认识的协议 %q", scheme)
		}
	}
	if rest == "" {
		return t, errors.New("地址为空")
	}
	host := rest
	if i := strings.IndexByte(host, '/'); i >= 0 {
		// an https URL: what follows the host is the endpoint, not part of the address
		t.Path = host[i:]
		host = host[:i]
	}
	switch {
	case strings.HasPrefix(host, "["):
		end := strings.IndexByte(host, ']')
		if end < 0 {
			return t, errors.New("IPv6 地址缺少右方括号")
		}
		t.Host = host[1:end]
		if k := strings.LastIndexByte(host, ':'); k > end {
			t.Port = host[k+1:]
		}
	case strings.Count(host, ":") == 1:
		if k := strings.IndexByte(host, ':'); k > 0 {
			t.Host, t.Port = host[:k], host[k+1:]
		}
	default:
		t.Host = host
	}
	if t.Host == "" {
		return t, errors.New("地址为空")
	}
	return t, nil
}

// Address is the resolver as a host:port pair, for dialing.
func (t target) Address() string { return net.JoinHostPort(t.Host, t.Port) }

// Options says how to reach the resolver and how long to wait.
type Options struct {
	// Proxy is the running core's mixed inbound, when a resolver is to be reached through
	// it. The foreign resolvers are: the core's own dns-remote carries detour: proxy, and a
	// measurement or a relay taken any other way would be a different path from the one
	// traffic actually uses.
	Proxy *url.URL
	// Timeout is one exchange's whole allowance.
	Timeout time.Duration
	// RetryTruncated re-asks over TCP when a plain answer came back truncated. True for a
	// relay, which owes its client a whole answer; false for a probe, whose whole question
	// is the round trip — retrying inside the measurement would time a second handshake as
	// if it were the first.
	RetryTruncated bool
	// InterfaceIndex pins the connection's egress to one interface, for a dial that must
	// not follow the machine's routes — the app's own forwarder asking a domestic resolver
	// while a tunnel is up above all (see internal/dnsstub). Zero is "let the system
	// choose", which is what every dial that is not that one wants.
	InterfaceIndex uint32
	// Bootstrap resolves the resolver's own host name, for an upstream written as a name
	// while the machine's system resolver may be the very path this query is for. A DoH
	// server named by host name cannot be dialled through the system resolver once that
	// resolver is this forwarder: the name would come back as a query to be forwarded, and
	// the answer would be asked of the path that is still trying to open. These are plain
	// DNS servers, used for nothing but that one lookup (see internal/dnsstub, which fills
	// them from the resolvers the machine had before the takeover). Empty keeps the
	// system resolver, which is right whenever nothing has taken it over.
	Bootstrap []string
}

func (o Options) withDefaults() Options {
	if o.Timeout <= 0 {
		o.Timeout = 3 * time.Second
	}
	return o
}

// Exchange sends one message to one resolver and returns its answer, on a connection of
// its own.
//
// This is the form for a question asked once — a probe, which is timing the handshake as
// much as the answer. A caller that asks the same resolver repeatedly wants Client
// instead: a resolver reached through the tunnel costs several round trips to reach, and
// paying them per question is what a kept-open connection is for.
func Exchange(ctx context.Context, t Target, msg []byte, opts Options) ([]byte, error) {
	c := NewClient(t, opts)
	defer c.Close()
	return c.Exchange(ctx, msg)
}

// Client is a kept-open path to one resolver: a connection for the stream transports, an
// HTTP client for DoH, a connected socket for plain UDP. One exchange at a time — the
// connection is shared, and two questions in flight on one stream would have to be told
// apart by their ids.
type Client struct {
	t    Target
	opts Options

	mu   sync.Mutex
	conn net.Conn
	http *http.Client
	// sem is what serialises exchanges, and it is a channel rather than mu for one
	// reason: a query can need this same client to answer something else first (an
	// upstream named by a host name has to be resolved before it can be dialled, and
	// that resolution may come back through the forwarder this client serves). A mutex
	// there is an unbounded wait with no way out; a receive on sem honours the caller's
	// deadline, so the second question fails in its own five seconds instead of hanging
	// behind a path that cannot open until it finishes.
	sem chan struct{}
}

// NewClient prepares a path to t. Nothing is dialled until the first exchange: a forwarder
// builds one of these when its upstream is set, and that may well be while no node is
// running and nothing can be reached at all.
func NewClient(t Target, opts Options) *Client {
	c := &Client{t: t, opts: opts.withDefaults(), sem: make(chan struct{}, 1)}
	c.sem <- struct{}{}
	return c
}

// Target is the resolver this client asks.
func (c *Client) Target() Target { return c.t }

// Exchange relays one message, serialised against the other users of this path.
func (c *Client) Exchange(ctx context.Context, msg []byte) ([]byte, error) {
	select {
	case <-c.sem:
		defer func() { c.sem <- struct{}{} }()
	case <-ctx.Done():
		// The path is busy and this question will not wait it out: the caller's deadline
		// is the answer, rather than a queue that can outlive it (see sem).
		return nil, ctx.Err()
	}

	ctx, cancel := context.WithTimeout(ctx, c.opts.Timeout)
	defer cancel()

	if c.t.Transport == HTTPS {
		return c.exchangeDoHOnce(ctx, msg)
	}
	reply, err := c.exchangeOnce(ctx, msg)
	if err != nil {
		// A kept-open connection can be dead without having said so — the resolver
		// closed it while it was idle, the tunnel was rebuilt under it. Dropping it and
		// asking once more is what makes the second question of a session work as well as
		// the first.
		c.dropConn()
		return c.exchangeOnce(ctx, msg)
	}
	return reply, nil
}

// exchangeOnce sends one message over the current connection, dialling it if there is not
// one yet.
func (c *Client) exchangeOnce(ctx context.Context, msg []byte) ([]byte, error) {
	conn, err := c.ensureConn(ctx)
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if c.streamed() {
		return exchangeStream(conn, msg)
	}
	reply, err := exchangeDatagram(conn, msg)
	if err != nil {
		return nil, err
	}
	if c.opts.RetryTruncated && truncated(reply) {
		// The rest is on a stream. Asked on a connection of its own rather than on this
		// one: this socket is a datagram socket, and the kept-open UDP path stays what it
		// is for the questions that fit.
		return exchangeOverTCP(ctx, c.t, msg, c.opts)
	}
	return reply, nil
}

// streamed reports whether this target is spoken to over a stream. A plain resolver behind
// the proxy is: CONNECT carries a stream, so plain DNS over the proxy means TCP.
func (c *Client) streamed() bool {
	return c.t.Transport == TCP || c.t.Transport == TLS || (c.t.Transport == UDP && c.opts.Proxy != nil)
}

// ensureConn returns the connection, dialling it if needed.
func (c *Client) ensureConn(ctx context.Context) (net.Conn, error) {
	if c.conn != nil {
		return c.conn, nil
	}
	network := "udp"
	if c.streamed() {
		network = "tcp"
	}
	conn, err := dial(ctx, network, c.t.Address(), c.opts)
	if err != nil {
		return nil, err
	}
	if c.t.Transport == TLS {
		conn = tls.Client(conn, &tls.Config{ServerName: c.t.Host})
	}
	c.conn = conn
	return c.conn, nil
}

// exchangeDoHOnce posts one message through the client's transport, which keeps its own
// connections and is built once.
func (c *Client) exchangeDoHOnce(ctx context.Context, msg []byte) ([]byte, error) {
	if c.http == nil {
		transport := &http.Transport{}
		if c.opts.Proxy != nil {
			transport.Proxy = http.ProxyURL(c.opts.Proxy)
		} else if c.opts.InterfaceIndex != 0 || len(c.opts.Bootstrap) > 0 {
			// The same dialer as dial's, for the transport that makes its own
			// connections: the egress pin and the bootstrap lookup both have to apply
			// here, or a DoH upstream named by host name is resolved through the very
			// forwarder it is serving.
			d := directDialer(c.opts)
			transport.DialContext = d.DialContext
		}
		c.http = &http.Client{Transport: transport, Timeout: c.opts.Timeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://"+c.t.Address()+c.t.Path, bytes.NewReader(msg))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 65535))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return body, nil
}

// dropConn forgets the connection without waiting to see whether it was usable.
func (c *Client) dropConn() {
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}

// Close puts the path away.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dropConn()
	if c.http != nil {
		c.http.CloseIdleConnections()
		c.http = nil
	}
	return nil
}

// exchangeDatagram writes the message and reads one answer back. The buffer is a
// datagram's maximum: an answer larger than the negotiated size arrives truncated with
// the flag set, and the decision about it belongs to the caller (see RetryTruncated).
func exchangeDatagram(conn net.Conn, msg []byte) ([]byte, error) {
	if _, err := conn.Write(msg); err != nil {
		return nil, err
	}
	buf := make([]byte, 65535)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

// exchangeStream writes the message with its length in front and reads the answer the
// same way — the framing is shared by plain DNS over TCP and by DoT.
func exchangeStream(conn net.Conn, msg []byte) ([]byte, error) {
	framed := make([]byte, 2, 2+len(msg))
	binary.BigEndian.PutUint16(framed, uint16(len(msg)))
	framed = append(framed, msg...)
	if _, err := conn.Write(framed); err != nil {
		return nil, err
	}
	var length [2]byte
	if _, err := io.ReadFull(conn, length[:]); err != nil {
		return nil, err
	}
	size := int(binary.BigEndian.Uint16(length[:]))
	if size < 12 || size > 65535 {
		return nil, errors.New("应答长度异常")
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, err
	}
	return body, nil
}

// exchangeOverTCP re-asks on a stream, which is where the rest of a truncated answer is.
// The proxy is deliberately not consulted for the transport choice again: a plain
// resolver behind the proxy was already asked over TCP, so this is reached only by a
// resolver spoken to directly.
func exchangeOverTCP(ctx context.Context, t Target, msg []byte, opts Options) ([]byte, error) {
	plain := t
	if plain.Transport == UDP {
		plain.Transport = TCP
	}
	conn, err := dial(ctx, "tcp", plain.Address(), opts)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if plain.Transport == TLS {
		conn = tls.Client(conn, &tls.Config{ServerName: plain.Host})
	}
	return exchangeStream(conn, msg)
}

// dial opens a connection, through the core's inbound when there is one to go through.
func dial(ctx context.Context, network, address string, opts Options) (net.Conn, error) {
	if opts.Proxy == nil {
		return directDialer(opts).DialContext(ctx, network, address)
	}
	if network != "tcp" {
		// CONNECT carries a byte stream. A plain resolver asked through the core would
		// need UDP carried over the proxy, which this does not do; the foreign resolvers
		// that are worth reaching are asked over TCP anyway (DoH and DoT).
		return nil, errors.New("明文 DNS 无法经代理转发")
	}
	return DialVia(ctx, opts.Proxy, address)
}

// directDialer is the dialer for a connection that does not go through the core: the
// machine's real egress where one is pinned, and a bootstrap resolver where the system one
// may be the path being served (see Options.Bootstrap). The proxy path is deliberately left
// unpinned — it dials the loopback, and pinning a loopback dial to a physical interface is
// how a working hop stops working.
func directDialer(opts Options) *net.Dialer {
	d := &net.Dialer{}
	if opts.InterfaceIndex != 0 {
		d.Control = bindInterface(opts.InterfaceIndex)
	}
	if len(opts.Bootstrap) > 0 {
		d.Resolver = bootstrapResolver(opts.Bootstrap)
	}
	return d
}

// bootstrapResolver asks the given plain resolvers, in turn, for the one name a dial needs:
// the upstream's own. Rotation rather than a fixed first — a reader whose old resolver is
// unreachable should not lose the upstream for that, and which one answers is not a
// property of the machine worth pinning.
func bootstrapResolver(servers []string) *net.Resolver {
	var mu sync.Mutex
	next := 0
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			mu.Lock()
			server := servers[next%len(servers)]
			next++
			mu.Unlock()
			var d net.Dialer
			return d.DialContext(ctx, "udp", bootstrapAddress(server))
		},
	}
}

// bootstrapAddress accepts "1.1.1.1" and "1.1.1.1:5353": a bare address is port 53, which
// is what the machine's own resolvers carry, and an explicit port is what a test needs to
// run its own server without taking that port.
func bootstrapAddress(server string) string {
	if _, _, err := net.SplitHostPort(server); err == nil {
		return server
	}
	return net.JoinHostPort(server, "53")
}

// DialVia opens a connection to address by asking an HTTP proxy to open it — CONNECT,
// which the core's mixed inbound answers because it speaks HTTP as well as SOCKS.
//
// A tunnel rather than the proxy's own address handling: what travels through it is not
// an HTTP request but a DNS message, and DoT is not HTTP at all.
func DialVia(ctx context.Context, proxy *url.URL, address string) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", proxy.Host)
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	req := "CONNECT " + address + " HTTP/1.1\r\nHost: " + address + "\r\n"
	if proxy.User != nil {
		pass, _ := proxy.User.Password()
		creds := base64.StdEncoding.EncodeToString([]byte(proxy.User.Username() + ":" + pass))
		req += "Proxy-Authorization: Basic " + creds + "\r\n"
	}
	req += "\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, err
	}

	// Read line by line rather than with http.ReadResponse: that would give a 2xx reply an
	// unbounded body, and the body would be the tunnel.
	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, err
	}
	if !strings.Contains(status, " 200") {
		conn.Close()
		return nil, fmt.Errorf("代理拒绝连接：%s", strings.TrimSpace(status))
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, err
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}

	// The reader may have taken bytes of the tunnel already; anything it holds has to come
	// along, so the connection is wrapped in one that reads from it first.
	if reader.Buffered() == 0 {
		return conn, nil
	}
	return &bufferedConn{Conn: conn, reader: reader}, nil
}

// bufferedConn is a connection whose first bytes were already read.
type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

// truncated reports whether an answer says that it did not fit.
func truncated(reply []byte) bool {
	if len(reply) < 4 {
		return false
	}
	// Flags live in the third and fourth bytes; TC is the second bit of the first of them.
	return reply[2]&0x02 != 0
}
