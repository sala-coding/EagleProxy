// Package tunfast replicates the fast bring-up path karing takes on Windows:
// the wintun driver DLL embedded in the binary and loaded straight from
// memory, a deterministic adapter GUID so an adapter left over from an
// earlier session is opened instead of re-created, and every bit of adapter
// configuration done through Win32 APIs — never by spawning netsh.
//
// What each choice buys, in the order it matters at start-up:
//
//   - memmod loading means the driver is never written to disk and never
//     searched for: the CreateAdapter call the adapter lives or dies on is
//     reached without touching the filesystem. A fetch from PATH or
//     System32 costs milliseconds at best and a missing file at worst,
//     usually surfacing as "wintun: Failed to setup adapter".
//   - md5("wintun"+name) as the adapter GUID keeps the identity stable
//     across restarts. wintun then fails CreateAdapter with
//     ERROR_OBJECT_ALREADY_EXISTS on an adapter that is already there, and
//     the fast path is OpenAdapter instead: creating an adapter is the
//     only step that can invoke the driver service, and skipping it is the
//     single largest constant in a restart. A stable GUID also keeps the
//     network-profile entry (NLA) the same, so Windows does not spend the
//     start re-classifying a "new" network.
//   - API-only configuration removes the child processes. One netsh run
//     costs hundreds of milliseconds before it answers, and a bring-up
//     that needs several serialises that into seconds. Win32 calls are
//     nanoseconds to microseconds and fail with a real errno instead of a
//     string to parse.
//
// This app never opens the packet session itself: the adapter device is
// created ahead of time by Prepare (the seconds-long PnP step, done while
// a process is already elevated) and opened afterwards by the sing-box
// core. What remains here is adapter identity and the route capture
// (Link.Take/Release, EgressIndex) the app drives directly.
//
// Bringing an adapter up requires the administrator token, exactly as TUN
// mode already holds it when the core launches.
package tunfast
