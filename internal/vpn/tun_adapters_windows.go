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

// staleAdapterScript removes the wintun adapters that are not working and
// prints how many it removed.
//
// Only adapters whose status is not OK are touched: a healthy wintun adapter
// belongs to whichever VPN is using it, and removing that would break a program
// that is working fine.
//
// The three names are the FriendlyName a broken adapter of this family answers
// with. 'wintun' is the driver's own entry (Wintun Userspace Tunnel);
// 'sing-tun' is the tunnel type this app's core creates the adapter with; and
// 'eagletun' is the connection name THIS app gives its adapter — the name the
// reader sees in 网络连接 — which a script matching only the two above would
// have left in place: the leftover's symptom ("Cannot create a file when that
// file already exists") is exactly what this sweep exists to clear, and this
// app's own adapter is the likeliest one to be found broken.
const staleAdapterScript = `$bad = @(Get-PnpDevice -Class Net -ErrorAction SilentlyContinue | Where-Object { ($_.FriendlyName -match 'wintun|sing-tun|eagletun') -and ($_.Status -ne 'OK') })
foreach ($d in $bad) { pnputil /remove-device "$($d.InstanceId)" 2>$null | Out-Null }
$left = @(Get-PnpDevice -Class Net -ErrorAction SilentlyContinue | Where-Object { ($_.FriendlyName -match 'wintun|sing-tun|eagletun') -and ($_.Status -ne 'OK') })
$removed = $bad.Count - $left.Count
if ($removed -lt 0) { $removed = 0 }
$removed`

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
// script returns is the honest one: devices found minus devices still broken
// afterwards. What actually clears a broken adapter is the elevated server's own
// adapter creation; a zero here says so rather than claiming a cleanup that did
// not happen.
func CleanStaleTunAdapters() (int, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", staleAdapterScript)
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
// pnputil rather than Get-PnpDevice for both the look and the removal. Its enumeration is
// the device tree itself, it takes an instance id as a filter, and it answers for devices
// in any state — while the CIM view a Get-PnpDevice query goes through does not: a wintun
// device was measured here as invisible to it for the whole time it was up, which is how
// a removal used to report "0 of 0" over a device that was sitting right there.
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
$found = 0
$removed = 0
if ((pnputil /enum-devices /instanceid "$id" 2>$null) -match 'Instance ID') { $found = 1 }
if ($found -eq 1) {
  pnputil /remove-device "$id" 2>$null | Out-Null
  if (-not ((pnputil /enum-devices /instanceid "$id" 2>$null) -match 'Instance ID')) { $removed = 1 }
}
'EAGLETUN {0} {1}' -f $removed, $found`, strings.ReplaceAll(adapterDeviceID(name), "'", "''"))
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
