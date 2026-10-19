package dnsx

import (
	"context"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"
)

// The bootstrap path exists for one situation: an upstream written as a host name while the
// machine's system resolver is the forwarder that upstream serves. This pins both halves of
// it — that the name is asked of the given server, and that its answer is what the dial
// then uses — without needing the machine's resolver to be taken over.
func TestBootstrapResolverAsksTheGivenServer(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer pc.Close()

	asked := make(chan string, 4)
	go func() {
		buf := make([]byte, 512)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			query := append([]byte(nil), buf[:n]...)
			asked <- questionName(query)
			_, _ = pc.WriteTo(answerFor(query, [4]byte{1, 2, 3, 4}), addr)
		}
	}()

	r := bootstrapResolver([]string{pc.LocalAddr().String()})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// ip4 only: the stub answers A questions, and a resolver asked for both families would
	// wait out the AAAA it never gets.
	ips, err := r.LookupIP(ctx, "ip4", "upstream.test")
	if err != nil {
		t.Fatalf("LookupIP through the bootstrap server: %v", err)
	}
	if len(ips) != 1 || ips[0].String() != "1.2.3.4" {
		t.Fatalf("addresses = %v, want the stub's 1.2.3.4", ips)
	}
	select {
	case name := <-asked:
		if name != "upstream.test" {
			t.Errorf("bootstrap server was asked for %q, want upstream.test", name)
		}
	default:
		t.Error("the bootstrap server was never asked")
	}
}

// bootstrapAddress accepts the two shapes the record can carry.
func TestBootstrapAddressShapes(t *testing.T) {
	if got := bootstrapAddress("1.1.1.1"); got != "1.1.1.1:53" {
		t.Errorf("bare address = %q, want it on port 53", got)
	}
	if got := bootstrapAddress("127.0.0.1:5353"); got != "127.0.0.1:5353" {
		t.Errorf("address with a port = %q, want it left alone", got)
	}
	if got := bootstrapAddress("::1"); got != "[::1]:53" {
		t.Errorf("bare IPv6 = %q, want it bracketed on port 53", got)
	}
}

// questionName reads the qname out of a query. Labels only: the stub is answering a name
// the test itself wrote, and compression pointers cannot appear in a question.
func questionName(q []byte) string {
	if len(q) < 13 {
		return ""
	}
	var labels []string
	for i := 12; i < len(q); {
		n := int(q[i])
		if n == 0 || n&0xC0 != 0 {
			break
		}
		if i+1+n > len(q) {
			break
		}
		labels = append(labels, string(q[i+1:i+1+n]))
		i += 1 + n
	}
	return strings.Join(labels, ".")
}

// answerFor builds the smallest possible valid answer: the question echoed back, plus one A
// record pointing at ip. Only the question is echoed — a query carrying an EDNS OPT record
// would otherwise leave the answer's additional section claiming a record that is not
// there, which the resolver reads as a malformed reply rather than as a name that has no
// address.
func answerFor(q []byte, ip [4]byte) []byte {
	end := 12
	for end < len(q) && q[end] != 0 {
		end += int(q[end]) + 1
	}
	end += 1 + 4 // root label, type, class
	if end > len(q) {
		end = len(q)
	}
	out := append([]byte(nil), q[:end]...)
	out[2] = 0x81 // response, recursion available
	out[3] = 0x80
	binary.BigEndian.PutUint16(out[4:6], 1)  // QDCOUNT
	binary.BigEndian.PutUint16(out[6:8], 1)  // ANCOUNT
	binary.BigEndian.PutUint16(out[8:10], 0) // NSCOUNT
	binary.BigEndian.PutUint16(out[10:12], 0)
	out = append(out, 0xC0, 0x0C)  // name: pointer to the question
	out = append(out, 0, 1, 0, 1)  // type A, class IN
	out = append(out, 0, 0, 0, 60) // TTL
	out = append(out, 0, 4)        // RDLENGTH
	out = append(out, ip[:]...)
	return out
}
