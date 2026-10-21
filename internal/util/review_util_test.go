package util

import (
	"strings"
	"testing"
)

// TestReviewExitIPServicesAreAllTLS pins the transport the address-echo
// services are asked over.
//
// The answer is only checked for being a well-formed address, so a cleartext
// answer could be any address at all — and the panel would show the machine as
// being somewhere it is not. One of the five was plain HTTP.
func TestReviewExitIPServicesAreAllTLS(t *testing.T) {
	if len(ipEchoURLs) == 0 {
		t.Fatal("there are no address-echo services at all")
	}
	for _, u := range ipEchoURLs {
		if !strings.HasPrefix(u, "https://") {
			t.Errorf("the address-echo service %q is not over TLS: a cleartext answer could be "+
				"any address at all, and the panel would show the machine as being somewhere it is not", u)
		}
	}
}
