package dnsstub

import (
	"net"
	"net/url"
	"testing"
)

// TestReviewSystemSidePartialBindIsNeverCompleted pins down what happens to a takeover
// whose port 53 was only half taken.
//
// bindSystem is written to fill in each address family that is missing — it checks
// haveUDP/haveTCP per family precisely so that a retry cannot take away what the
// previous attempt installed. But the only caller, setSystem, calls it solely when
// systemServingLocked() is false, and that reports true as soon as ANY of the four
// sockets exists. A partial bind is therefore never completed: the three missing
// halves stay missing for the life of the process, while SystemReady() keeps saying
// the takeover is up and the app points the machine's interfaces at 127.0.0.1:53.
//
// The state below is the one a partial bind leaves behind, reproduced without needing
// port 53: the UDP v4 socket bound, the other three missing.
func TestReviewSystemSidePartialBindIsNeverCompleted(t *testing.T) {
	s := startOn(0, 0, nil)
	defer s.Close()

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe socket: %v", err)
	}
	s.mu.Lock()
	s.sysU4 = pc
	s.mu.Unlock()

	proxy, err := url.Parse("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("proxy url: %v", err)
	}

	// What the app does on every reconcile while a node is running.
	s.setSystem(Wires{System: true, Proxy: func() *url.URL { return proxy }})

	s.mu.Lock()
	tcp4 := s.listens[System]
	tcp6 := s.sysV6
	udp6 := s.sysU6
	udp4 := s.sysU4
	s.mu.Unlock()

	if tcp4 == nil {
		t.Errorf("TCP 127.0.0.1:53 was never bound after setSystem: the partial bind was not completed")
	}
	if tcp6 == nil {
		t.Errorf("TCP [::1]:53 was never bound after setSystem: the partial bind was not completed")
	}
	if udp6 == nil {
		t.Errorf("UDP [::1]:53 was never bound after setSystem: the partial bind was not completed")
	}
	if udp4 == nil {
		t.Errorf("UDP 127.0.0.1:53 was never bound after setSystem: the partial bind was not completed")
	}

	// And the app is told everything is fine.
	if !s.SystemReady() {
		t.Errorf("SystemReady() is false, so this scenario is not reachable")
	}
}
