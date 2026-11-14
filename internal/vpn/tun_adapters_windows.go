//go:build windows

package vpn

import (
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"eagle/internal/util"
)

// staleAdapterScript removes this app's own wintun device when it is left in a
// state that cannot be opened, and prints 1 if it really went and 0 if not.
//
// The sweep is by instance id — the id adapterDeviceID derives from this app's
// connection name — and not by the shape of the id. The shape version
// (ROOT\WINTUN\* and SWD\Wintun\*) matched every wintun device on the machine,
// which is not a family this app can claim: another VPN client's idle adapter
// answers to the same shape and is removed by the same line, and the reader's
// other tunnel disappears without anything having asked it to. The id is the one
// thing only this app's adapter carries.
//
// pnputil and not Get-PnpDevice: the CIM view behind Get-PnpDevice does not show
// wintun devices at all — a live adapter was measured as invisible to it for the
// whole time it was up — so the earlier sweep answered "0 of 0" over leftovers
// that were sitting right there in the device tree. pnputil reads the tree
// itself and answers for devices in any state. The look afterwards is the class
// enumeration rather than /instanceid, for the reason the removal itself learned:
// the filter answers only for devices the hardware tree still carries, and a
// leftover is exactly the device that is no longer in it.
//
// Only a device that is not Started is touched. A Started device is one a core is
// holding open this minute, and removing that would break a tunnel that works; a
// leftover never gets that far (measured: a device whose core died answers
// Disconnected).
//
// A phantom node — a registry entry with no hardware tree behind it — cannot be
// removed by pnputil at all, so the answer is 0 rather than a claimed cleanup.
func staleAdapterScript(name string) string {
	return fmt.Sprintf(`$id = '%s'
$cur = ''
$status = ''
$bad = 0
foreach ($line in (pnputil /enum-devices /class Net 2>$null)) {
  if ($line -match '^Instance ID:\s+(.+?)\s*$') {
    $line_id = $matches[1]
    if ($cur -and $status -ne 'Started' -and $cur -eq $id) { $bad = 1 }
    $cur = $line_id
    $status = ''
  } elseif ($line -match '^Status:\s+(.+?)\s*$') {
    $status = $matches[1]
  }
}
if ($cur -and $status -ne 'Started' -and $cur -eq $id) { $bad = 1 }
if ($bad -eq 1) {
  pnputil /remove-device "$id" 2>$null | Out-Null
  if (pnputil /enum-devices /class Net 2>$null | Select-String -SimpleMatch $id) { '0' } else { '1' }
} else { '0' }`, strings.ReplaceAll(adapterDeviceID(name), "'", "''"))
}

// CleanStaleTunAdapters removes wintun adapters left in a broken state by an
// earlier core and reports how many it removed.
//
// sing-box creates its adapter through wintun, and a core that is killed — or
// that crashes — part-way through that can leave the adapter registered but
// unusable. Every later start then fails with the pair of errors
//
//	create adapter: Cannot create a file when that file already exists.
//	open existing adapter: Element not found.
//
// which reads like a driver fault but is really a leftover: the name is taken,
// the device cannot be opened. Nothing else in the app can clear that, so it is
// done here, before the core is launched.
//
// Removing a device needs the administrator token. In the two-process shape the
// app is deliberately unelevated — the token belongs to the resident server — so
// a removal here usually fails with the error swallowed above, and the count the
// script returns is the honest one: each device it removed is re-enumerated to
// prove it is gone. What actually clears a broken adapter is the elevated
// server's own adapter creation; a zero here says so rather than claiming a
// cleanup that did not happen.
func CleanStaleTunAdapters(name string) (int, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", staleAdapterScript(name))
	util.HideConsole(cmd)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("检查残留 TUN 网卡失败: %w", err)
	}
	n, convErr := strconv.Atoi(strings.TrimSpace(string(out)))
	if convErr != nil {
		// The script printed something unexpected; nothing to act on.
		return 0, nil
	}
	return n, nil
}

// adapterDeviceID is the PnP instance id of the adapter wintun creates for this
// connection name: SWD\Wintun\{GUID}, with the GUID derived as md5("wintun"+name) — the
// derivation tunfast hands CreateAdapter so an adapter survives a restart.
//
// Naming the device this way is what makes the removal exact AND status-independent. A
// connection-name lookup (Get-NetAdapter) only answers for a device Windows still calls a
// network adapter, and a wintun device whose core has just been killed drops out of that
// view entirely: it is found as "0 of 0" while the device is very much still in the tree,
// waiting to break the next start. The instance id is there whatever the status says.
func adapterDeviceID(name string) string {
	sum := md5.Sum([]byte("wintun" + name))
	return fmt.Sprintf(`SWD\Wintun\{%08X-%04X-%04X-%02X%02X-%02X%02X%02X%02X%02X%02X}`,
		binary.LittleEndian.Uint32(sum[0:4]),
		binary.LittleEndian.Uint16(sum[4:6]),
		binary.LittleEndian.Uint16(sum[6:8]),
		sum[8], sum[9], sum[10], sum[11], sum[12], sum[13], sum[14], sum[15])
}

// removeAdapterScript removes the device with this instance id — present, phantom or
// broken alike — and prints how many devices went, and how many were there.
//
// Unlike staleAdapterScript this one does not ask about status. The caller has decided
// the device is not wanted at all — TUN was switched off and nothing behind it needs a
// capture — and a HEALTHY device is exactly what is then left to remove.
//
// pnputil rather than Get-PnpDevice for both the look and the removal: the CIM view a
// Get-PnpDevice query goes through does not show wintun devices at all — measured, a live
// adapter was invisible to it for the whole time it was up — which is how a removal used
// to report "0 of 0" over a device that was sitting right there.
//
// The look is a scan of the class enumeration and not the /instanceid filter of the same
// command. The filter only answers for devices the hardware tree still carries; a leftover
// — the exact thing this call exists to clear — is in the enumeration but not in the tree,
// so the filter reported "removed 0 of 0" over it and the caller concluded there was
// nothing to do (measured on this machine, over a leftover the enumeration listed).
//
// Nothing here is matched by friendly name. A wintun device answers with the driver's
// ("sing-tun Tunnel" here), which every other sing-box adapter on the machine shares; the
// instance id belongs to this name alone, so another program's adapter cannot be caught.
//
// The id is escaped for a single-quoted PowerShell string rather than passed through,
// because this is one of the few places where a value reaches a shell.
//
// It prints a marked line with two numbers, removed then found, because the caller has
// two different things to say: "there was nothing there" is not a failure, and "there was
// something and it stayed" is. A single count cannot tell them apart, and output that
// carries no marked line at all is reported as a failure instead of read as zero.
func removeAdapterScript(name string) string {
	return fmt.Sprintf(`$id = '%s'
$look = { @(pnputil /enum-devices /class Net 2>$null | Select-String -SimpleMatch $id).Count -gt 0 }
$found = 0
$removed = 0
if (& $look) { $found = 1 }
if ($found -eq 1) {
  pnputil /remove-device "$id" 2>$null | Out-Null
  if (-not (& $look)) { $removed = 1 }
}
'EAGLETUN {0} {1}' -f $removed, $found`, strings.ReplaceAll(adapterDeviceID(name), "'", "''"))
}

// removeAdapterDirect asks pnputil to remove this app's device, and reports 1 when it
// really did (with ok true) and 0 otherwise.
//
// No shell: pnputil is a console program of its own, and going through PowerShell to
// reach it is where the seconds went (see RemoveTunAdapter). The call reports success
// by exit code, which is the only thing that can be read without enumerating the class
// a second time — and enumerating it is the slow half of the script.
//
// A device that is not in the hardware tree answers an error here, and that error is
// also what a refused removal looks like, so this says ok only on success and lets the
// caller's script decide what the failure meant.
func removeAdapterDirect(name string) (int, bool) {
	if _, err := exec.LookPath("pnputil"); err != nil {
		return 0, false
	}
	cmd := exec.Command("pnputil", "/remove-device", adapterDeviceID(name))
	util.HideConsole(cmd)
	if err := cmd.Run(); err != nil {
		return 0, false
	}
	return 1, true
}

// RemoveTunAdapter removes this app's adapter and reports how many devices went with it,
// and how many were there to begin with.
//
// This is the ending a TUN switch-off asks for: the device is a network connection in
// the reader's own list, and one left behind outlives the mode that made it. It is a
// PnP removal, which is why the app cannot do it itself — the token belongs to the
// resident server — and why an unelevated call answers a zero rather than an error: the
// count is "devices found minus devices still there", the honest reading of a removal
// that was refused.
//
// The front-end has to be gone first. A running core holds the device open and pnputil
// is refused; the caller is the one that kills it, and the wait for its exit is
// there too.
func RemoveTunAdapter(name string) (removed int, found int, err error) {
	if name == "" {
		return 0, 0, fmt.Errorf("移除 TUN 网卡失败: 未指定网卡名称")
	}
	// One pnputil call and no shell, for the case that actually happens: the device is
	// there and the caller holds the token. Measured on this machine, the PowerShell
	// route below costs five to six seconds — a host start, a class enumeration, the
	// removal, and a second enumeration to prove it — and the reader who just switched
	// TUN off is watching the connection for it (they timed it: "2 秒后应该消失").
	// pnputil answers the same removal in well under a second.
	//
	// Anything that is not a clean success falls through to the script, which is the
	// one that can tell "there was nothing there" apart from "it stayed": a phantom
	// device answers an error here, and the refused-removal count is the script's job.
	if gone, ok := removeAdapterDirect(name); ok {
		return gone, gone, nil
	}
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", removeAdapterScript(name))
	util.HideConsole(cmd)
	out, err := cmd.Output()
	if err != nil {
		return 0, 0, fmt.Errorf("移除 TUN 网卡失败: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 3 || fields[0] != "EAGLETUN" {
			continue
		}
		removed, rErr := strconv.Atoi(fields[1])
		found, fErr := strconv.Atoi(fields[2])
		if rErr != nil || fErr != nil {
			break
		}
		return removed, found, nil
	}
	// Output that carries no marked line means the query itself did not run — a
	// PowerShell that refused the command, a module that failed to load. Reporting zero
	// here is what made a refused removal look like "there was nothing to do".
	return 0, 0, fmt.Errorf("移除 TUN 网卡失败: 无法解析脚本输出 %.200q", strings.TrimSpace(string(out)))
}
