package dnsx_test

import (
	. "eagle/internal/dnsx"

	"context"
	"sync"
	"testing"
	"time"
)

// The forwarder puts a client away on its own goroutine while a question may
// still be on it (see dnsstub.closeClient). So Close and Exchange ran on two
// goroutines at once, and the connection field was read and written by both of
// them without a lock.
//
// Two things came out of it: a socket that was dialled after the close and
// never closed again, and a closed socket handed to a question that was still
// running. The race detector sees it as a race on the field; the second is
// what a reader sees as a SERVFAIL that came out of nowhere.
func TestCloseDuringExchangeDoesNotRace(t *testing.T) {
	addr, _, stop := fakeTCPResolver(t)
	defer stop()

	target, err := Parse("tcp://" + addr)
	if err != nil {
		t.Fatal(err)
	}
	c := NewClient(target, Options{Timeout: time.Second})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(id uint16) {
			defer wg.Done()
			// The error is not the point here: the point is that the close and
			// the exchange touch the same field at the same time.
			_, _ = c.Exchange(context.Background(), testQuery(id))
		}(uint16(i))
		go func() {
			defer wg.Done()
			_ = c.Close()
		}()
	}
	wg.Wait()
}

// A closed path is a path that is gone. It must not dial a fresh one: the
// socket would be handed to nobody, and nothing would ever close it.
func TestExchangeAfterCloseDoesNotDial(t *testing.T) {
	addr, dials, stop := fakeTCPResolver(t)
	defer stop()

	target, err := Parse("tcp://" + addr)
	if err != nil {
		t.Fatal(err)
	}
	c := NewClient(target, Options{Timeout: time.Second})
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := c.Exchange(context.Background(), testQuery(1)); err == nil {
		t.Error("the exchange on a closed path answered, want an error")
	}
	if n := dials(); n != 0 {
		t.Errorf("dialled %d times on a closed path, want 0", n)
	}
}
