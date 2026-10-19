//go:build windows

package tunfast

import (
	"errors"
	"fmt"
	"net/netip"

	"eagle/internal/tunfast/internal/winipcfg"

	"golang.org/x/sys/windows"
)

// AdapterResolvers is one interface's resolver list as the machine has it: what a snapshot
// is made of, and what "put it back" means.
//
// JSON-shaped because the snapshot outlives the process that took it. Whoever changes the
// machine's resolvers writes this down first, so a crash between the two leaves a way back
// rather than a machine asking a socket nobody owns.
type AdapterResolvers struct {
	Name string   `json:"name"`
	LUID uint64   `json:"luid"`
	V4   []string `json:"v4,omitempty"`
	V6   []string `json:"v6,omitempty"`
}

// SystemResolvers reads the resolver lists of the interfaces Windows would ask: every
// interface that carries a default route, with the tunnel passed over.
//
// These lists are the leak this exists for. Windows asks every interface that has a way out
// and takes the first answer that arrives, so making the tunnel's own resolver correct is
// not enough on its own: a router answering in five milliseconds beats a tunnel answering
// in a hundred every time — and the answer it wins with is a forged one for exactly the
// names the reader cares about (measured on this machine: www.google.com answered with a
// Facebook address, and the browser's handshake closed mid-flight). The tunnel's own
// adapter is skipped: its resolver is SelfDNS's business, and rewriting it here would be
// two writers on one field.
func SystemResolvers(tunnelName string) ([]AdapterResolvers, error) {
	skip := winipcfg.LUID(0)
	if tunnelName != "" {
		if l, err := Find(tunnelName); err == nil {
			skip = l.luid
		}
	}
	alias := aliasByLUID()

	seen := make(map[winipcfg.LUID]bool)
	var out []AdapterResolvers
	for _, family := range []winipcfg.AddressFamily{windows.AF_INET, windows.AF_INET6} {
		rows, err := winipcfg.GetIPForwardTable2(family)
		if err != nil {
			continue
		}
		for i := range rows {
			luid := rows[i].InterfaceLUID
			if luid == 0 || (skip != 0 && luid == skip) || seen[luid] {
				continue
			}
			prefix := rows[i].DestinationPrefix.Prefix()
			if !prefix.IsValid() || !prefix.Addr().IsUnspecified() || prefix.Bits() != 0 {
				continue // not a default route: nothing resolves through it
			}
			seen[luid] = true
			addrs, err := luid.DNS()
			if err != nil {
				continue
			}
			e := AdapterResolvers{Name: alias[luid], LUID: uint64(luid)}
			for _, a := range addrs {
				if a.Is4() {
					e.V4 = append(e.V4, a.String())
				} else if a.Is6() {
					e.V6 = append(e.V6, a.String())
				}
			}
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("tunfast: no interface with a default route to point anywhere")
	}
	return out, nil
}

// PrependResolvers puts one address in front of every list in the snapshot, keeping what
// the machine already had.
//
// In front of, not instead of, and that is the safety property of the whole arrangement: an
// interface keeps its own resolvers behind ours, so an answer that never comes — this
// process gone, the core stopped, a socket nobody is listening on — costs a timeout and
// then resolves the ordinary way. A list replaced outright would instead leave a machine
// that cannot resolve a name at all, which is a worse failure than the leak being fixed.
//
// A family with no resolvers is left alone: there is nothing to stand in front of, and
// putting one there would hand an interface an IPv6 resolver it never had.
func PrependResolvers(snapshot []AdapterResolvers, v4, v6 netip.Addr) error {
	var firstErr error
	for _, e := range snapshot {
		for _, f := range []struct {
			af    winipcfg.AddressFamily
			first netip.Addr
			had   []string
		}{
			{windows.AF_INET, v4, e.V4},
			{windows.AF_INET6, v6, e.V6},
		} {
			if !f.first.IsValid() || len(f.had) == 0 {
				continue
			}
			servers := make([]netip.Addr, 0, len(f.had)+1)
			servers = append(servers, f.first)
			// A snapshot taken while the takeover was already in place carries our own
			// loopback in front; prepending it again would list the same server twice,
			// and a retry (see the re-apply in tunserv) would grow the list each time.
			for _, a := range parseAddrList(f.had) {
				if a == f.first {
					continue
				}
				servers = append(servers, a)
			}
			if err := winipcfg.LUID(e.LUID).SetDNS(f.af, servers, nil); err != nil {
				firstErr = firstProblem(firstErr, fmt.Errorf("tunfast: %s: point resolvers here: %w", name(e), err))
			}
		}
	}
	return firstErr
}

// RestoreResolvers puts every list back exactly as the snapshot has it.
func RestoreResolvers(snapshot []AdapterResolvers) error {
	var firstErr error
	for _, e := range snapshot {
		for _, f := range []struct {
			af  winipcfg.AddressFamily
			had []string
		}{
			{windows.AF_INET, e.V4},
			{windows.AF_INET6, e.V6},
		} {
			if len(f.had) == 0 {
				continue
			}
			if err := winipcfg.LUID(e.LUID).SetDNS(f.af, parseAddrList(f.had), nil); err != nil {
				firstErr = firstProblem(firstErr, fmt.Errorf("tunfast: %s: restore resolvers: %w", name(e), err))
			}
		}
	}
	return firstErr
}

func parseAddrList(list []string) []netip.Addr {
	out := make([]netip.Addr, 0, len(list))
	for _, s := range list {
		if a, err := netip.ParseAddr(s); err == nil {
			out = append(out, a)
		}
	}
	return out
}

// firstProblem keeps the first failure and lets the rest of the list be attempted: one
// interface Windows will not let this process write is not a reason to leave the others
// pointing at a resolver that answers with forgeries.
func firstProblem(first, err error) error {
	if first == nil {
		return err
	}
	return first
}

func name(e AdapterResolvers) string {
	if e.Name == "" {
		return fmt.Sprintf("interface %d", e.LUID)
	}
	return e.Name
}

// aliasByLUID names interfaces the way the reader sees them, for lines that have to be
// actionable. Read once for a whole snapshot: the table read is not free, and every entry
// would otherwise pay for it.
func aliasByLUID() map[winipcfg.LUID]string {
	out := make(map[winipcfg.LUID]string)
	rows, err := winipcfg.GetIfTable2Ex(winipcfg.MibIfEntryNormal)
	if err != nil {
		return out
	}
	for i := range rows {
		out[rows[i].InterfaceLUID] = rows[i].Alias()
	}
	return out
}
