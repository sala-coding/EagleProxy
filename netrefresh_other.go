//go:build !windows

package main

// refreshSystemNetworkState is a Windows answer to a Windows question: NCSI is
// the system that shows an adapter as "no Internet" on its own clock. Nowhere
// else asks.
func refreshSystemNetworkState() {}
