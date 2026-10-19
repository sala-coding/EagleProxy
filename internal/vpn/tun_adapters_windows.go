//go:build windows

package vpn

import (
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
const staleAdapterScript = `$bad = @(Get-PnpDevice -Class Net -ErrorAction SilentlyContinue | Where-Object { ($_.FriendlyName -match 'wintun|sing-tun') -and ($_.Status -ne 'OK') })
foreach ($d in $bad) { pnputil /remove-device "$($d.InstanceId)" | Out-Null }
$bad.Count`

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
// Removing a device needs the administrator token, which TUN mode already holds
// by the time this runs.
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
