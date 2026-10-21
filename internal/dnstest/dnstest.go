// Package dnstest measures how quickly a resolver answers.
//
// It exists for one button: the DNS card's smart pick, which chooses the resolver to use by
// asking every candidate and keeping the fastest. The core cannot be asked to do this — its
// own API answers about the resolver it is already configured with — so the question is
// built and sent from here.
//
// The transports are not built here: they live in internal/dnsx, which the app's own
// forwarder shares (see internal/dnsstub). What is this package's own is the question — one
// fixed name, sixteen bytes of header, the name in labels and two more — and the timing.
package dnstest

import (
	"context"
	"encoding/binary"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"

	"eagle/internal/dnsx"
)

// probeName is what every probe asks for. A name that resolves everywhere and is blocked
// nowhere: the answer is not what is being measured, the round trip is. The same name for
// every resolver is also the honest model — a resolver's work is mostly the names it has
// already been asked for.
const probeName = "www.example.com"

// Result is one resolver's answer. Ms is the fastest of the probes that came back; Err says
// why there was none when there was none.
type Result struct {
	Server string `json:"server"`
	Ms     int64  `json:"ms"`
	Err    string `json:"err,omitempty"`
}

// Options says how to reach the resolvers, how long to wait, and how many to ask at once.
type Options struct {
	// Proxy is the running core's mixed inbound, when the resolvers are to be reached
	// through it. The foreign resolvers are: their server carries detour: proxy, so the
	// core reaches them over the tunnel, and a measurement taken any other way would be a
	// measurement of a path nothing uses.
	Proxy *url.URL
	// Timeout is one attempt's whole allowance.
	Timeout time.Duration
	// Parallel is how many resolvers are asked at the same time.
	Parallel int
	// Attempts is how many times each is asked; the fastest is the answer. More than one
	// because a single probe cannot tell a fast resolver from a lucky one, and the
	// difference being decided here is often a few milliseconds.
	Attempts int
	// InterfaceIndex pins the probe's connection to one interface — the machine's
	// real egress, with the tunnel passed over.
	//
	// It is what the domestic side is measured on, because that is the way the
	// forwarder reaches it (see internal/dnsstub): straight out of the physical
	// interface, past the tunnel. A probe that took the machine's ordinary route
	// instead would fall into the tunnel it is serving, be handed to the core, and
	// be answered by the core's own rules — so it would be measuring the wrong
	// path, and one that can fail for reasons that have nothing to do with the
	// resolver.
	//
	// Zero means "let the system choose", which is what the foreign side wants:
	// its resolvers are reached through the node, and a probe through the node is
	// a measurement of the path traffic actually uses.
	InterfaceIndex uint32
	// Bootstrap resolves a resolver named by host name, for the same reason
	// internal/dnsx has it: while this machine's own resolver is the forwarder
	// itself, the name would come back as a question to be forwarded, and the
	// answer would be asked of the path that is still trying to open.
	Bootstrap []string
}

func (o Options) withDefaults() Options {
	if o.Timeout <= 0 {
		o.Timeout = 1500 * time.Millisecond
	}
	if o.Parallel <= 0 {
		o.Parallel = 8
	}
	if o.Attempts <= 0 {
		o.Attempts = 3
	}
	return o
}

// Run asks every address in servers and returns one result each, in the order given.
//
// Nothing here fails: a resolver that will not answer is a resolver that cannot be the
// fastest, which is a result and not an error.
func Run(ctx context.Context, servers []string, opts Options) []Result {
	opts = opts.withDefaults()
	out := make([]Result, len(servers))
	sem := make(chan struct{}, opts.Parallel)
	var wg sync.WaitGroup

	for i, addr := range servers {
		out[i].Server = addr
		wg.Add(1)
		go func(i int, addr string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				out[i].Err = ctx.Err().Error()
				return
			}
			defer func() { <-sem }()
			ms, err := measure(ctx, addr, opts)
			if err != nil {
				out[i].Err = err.Error()
				return
			}
			out[i].Ms = ms
		}(i, addr)
	}
	wg.Wait()
	return out
}

// measure asks one resolver, opts.Attempts times, and keeps the fastest answer.
func measure(ctx context.Context, addr string, opts Options) (int64, error) {
	target, err := resolveTarget(addr)
	if err != nil {
		return 0, err
	}
	best := int64(0)
	// Whether an answer came back at all. It is a fact of its own, kept apart
	// from the time it took: 0 is the reading of a whole millisecond, and a
	// round trip shorter than that would read as 0 too — so 0 on its own cannot
	// say "nothing came back".
	answered := false
	var last error
	for attempt := 0; attempt < opts.Attempts; attempt++ {
		started := time.Now()
		err := ask(ctx, target, opts)
		if err != nil {
			last = err
			continue
		}
		// The elapsed time in whole milliseconds, rounded UP. A round trip that
		// took less than a millisecond is still an answer, and truncating it to
		// 0 would make it read as the one thing it is not: an unanswered
		// resolver. The local resolver — the fastest one on the machine, and the
		// one the card exists to find — answers in well under a millisecond.
		ms := time.Since(started).Milliseconds()
		if ms == 0 {
			ms = 1
		}
		if !answered || ms < best {
			best = ms
		}
		answered = true
	}
	if !answered {
		return 0, last
	}
	return best, nil
}

// resolveTarget reads a resolver address with the shared reader, and keeps the one refusal
// that is this package's own: `local` — the system's own resolver — is a keyword for
// "whatever the machine is set to", which is not a resolver to compare against the others.
func resolveTarget(addr string) (dnsx.Target, error) {
	if strings.EqualFold(strings.TrimSpace(addr), "local") {
		return dnsx.Target{}, errors.New("local 是系统解析器，不参与测速")
	}
	return dnsx.Parse(addr)
}

// ask sends one probe.
//
// RetryTruncated stays off: a probe is timing the question it asked, and a retry over TCP
// inside it would time a second handshake as if it were the first. What comes back
// truncated is still an answer to this question, which is all checkReply asks of it.
func ask(ctx context.Context, t dnsx.Target, opts Options) error {
	id := uint16(time.Now().UnixNano())
	reply, err := dnsx.Exchange(ctx, t, query(id), dnsx.Options{
		Proxy:   opts.Proxy,
		Timeout: opts.Timeout,
		// The pin and the bootstrap lookup, when the probe is not going through
		// the core: a probe that rides the machine's own routes would be answered
		// by the core it is measuring (see Options.InterfaceIndex).
		InterfaceIndex: opts.InterfaceIndex,
		Bootstrap:      opts.Bootstrap,
	})
	if err != nil {
		return err
	}
	return checkReply(reply, id)
}

// query builds one DNS message: a standard query for probeName, type A.
func query(id uint16) []byte { return queryFor(probeName, id) }

// queryFor builds one DNS message: a standard query for the given name, type A.
func queryFor(name string, id uint16) []byte {
	b := make([]byte, 0, 32+len(name))
	b = binary.BigEndian.AppendUint16(b, id)
	b = binary.BigEndian.AppendUint16(b, 0x0100) // standard query, recursion desired
	b = binary.BigEndian.AppendUint16(b, 1)      // one question
	b = binary.BigEndian.AppendUint16(b, 0)      // no answers
	b = binary.BigEndian.AppendUint16(b, 0)      // no authority
	b = binary.BigEndian.AppendUint16(b, 0)      // no additional
	for _, label := range strings.Split(name, ".") {
		b = append(b, byte(len(label)))
		b = append(b, label...)
	}
	b = append(b, 0)                        // end of the name
	b = binary.BigEndian.AppendUint16(b, 1) // type A
	b = binary.BigEndian.AppendUint16(b, 1) // class IN
	return b
}

// Query is the probe message for callers outside this package: one standard query for
// probeName, type A. The forwarder's warm-up sends it over a side's own client, so the
// connection the first real query would have to dial is already there.
func Query(id uint16) []byte { return query(id) }

// QueryFor is the same message for a name of the caller's own: one standard query,
// type A. A caller that has a name of its own to ask — the system's own connectivity
// probe asks for a particular name — needs the question more than it needs the
// measuring this package is for.
func QueryFor(name string, id uint16) []byte { return queryFor(name, id) }

// checkReply is the whole of what is asked of an answer: that it is an answer, and that it
// is this question's. What it says is beside the point — the time it took to say it is not.
func checkReply(body []byte, id uint16) error {
	if len(body) < 12 {
		return errors.New("应答过短")
	}
	if binary.BigEndian.Uint16(body[:2]) != id {
		return errors.New("应答的编号不匹配")
	}
	return nil
}
