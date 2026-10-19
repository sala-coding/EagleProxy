package sub

import (
	"encoding/base64"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A subscription panel usually knows what to call the subscription it serves —
// it is the name the user gave it on the panel — and several of them will say
// so in the response, but only to a client whose User-Agent they recognise.
// These are the places that name turns up, in the order they are worth
// believing, plus the rule for when a name may be replaced.

// nameFromResponse reads a subscription name out of the response headers and
// returns "" when the panel did not name itself.
//
// profile-title is what Clash.Meta, mihomo and Stash send: the name, base64
// encoded, behind a "base64:" prefix in the current spelling and bare in the
// older one. It is the only header a panel fills in for an ordinary
// subscription, so it is read first. Content-Disposition (RFC 6266) is the
// fallback: a panel that serves the subscription as a file names it there.
// Nothing else is guessed at — a header nobody is known to send is a guess that
// only ever produces false confidence.
func nameFromResponse(h http.Header) string {
	if len(h) == 0 {
		return ""
	}
	if v := h.Get("profile-title"); strings.TrimSpace(v) != "" {
		if name := decodeTitle(v); name != "" {
			return name
		}
	}
	// Content-Disposition: attachment; filename="HK 机场.yaml"
	if cd := h.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if name := tidyName(params["filename"]); name != "" {
				// The extension says nothing about the subscription and is
				// exactly the kind of noise a name exists to be free of.
				return strings.TrimSuffix(name, path.Ext(name))
			}
		}
	}
	return ""
}

// decodeTitle unwraps the profile-title header. The current spelling is
// "base64:<payload>", the older one is the payload alone, and a panel with
// nothing to encode sends the name as it is.
func decodeTitle(raw string) string {
	v := strings.TrimSpace(raw)
	if rest, ok := strings.CutPrefix(v, "base64:"); ok {
		return decodeBase64(strings.TrimSpace(rest))
	}
	// No prefix: try the encoding before trusting the text, because a plain name
	// like "Test" is also a valid base64 payload and would decode to noise. The
	// decoded bytes have to read as text for the guess to be accepted.
	if name := decodeBase64(v); name != "" {
		return name
	}
	return tidyName(v)
}

// decodeBase64 tries the four base64 spellings panels use, since they disagree
// about padding and about the URL-safe alphabet. A result that does not read as
// text is not a name, so it is discarded rather than shown as mojibake.
func decodeBase64(v string) string {
	if v == "" {
		return ""
	}
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		raw, err := enc.DecodeString(v)
		if err == nil && readable(raw) {
			return tidyName(string(raw))
		}
	}
	return ""
}

// readable reports whether the bytes are text a name could be made of. Base64
// of binary decodes cleanly often enough that accepting any decode would turn
// ordinary names into noise.
func readable(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, r := range string(b) {
		if r == utf8.RuneError || (unicode.IsControl(r) && r != '\t') {
			return false
		}
	}
	return true
}

// tidyName trims what a header carries around the name itself: the padding, a
// folded newline, and a length cap. The cap is generous but finite — a group
// list is scanned by name, and a name long enough to wrap its own cell is not a
// name.
func tidyName(s string) string {
	name := strings.TrimSpace(s)
	if i := strings.IndexAny(name, "\r\n"); i >= 0 {
		name = strings.TrimSpace(name[:i])
	}
	const maxName = 64
	if n := utf8.RuneCountInString(name); n > maxName {
		name = strings.TrimSpace(string([]rune(name)[:maxName])) + "…"
	}
	return name
}

// HostLabel is the placeholder name a subscription carries before the panel has
// been asked: the first label of the URL's host. A group list is scanned by
// name, and "example-sub" reads as an identifier where
// "example-sub.example.com:8443" is just noise. Hostname() drops the port, and the host
// is lowercased so this
// matches what the subscription dialog shows while the address is being typed.
func HostLabel(rawURL string) string {
	s := strings.TrimSpace(rawURL)
	if s == "" {
		return ""
	}
	// A pasted subscription address often arrives without a scheme ("host/sub?token"),
	// which url.Parse reads as a path rather than an authority. Prefixing one says
	// what the user meant, and is what the dialog does with the same string.
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return ""
	}
	if first, _, found := strings.Cut(host, "."); found && first != "" {
		return first
	}
	return host
}

// isAutoName reports whether the group's name is still the placeholder its URL
// produced, which is what makes replacing it safe. A name the user typed is
// theirs: a panel that renamed the subscription on every update would otherwise
// quietly take it over the first time the user refreshed it.
func isAutoName(name, rawURL string) bool {
	n := strings.TrimSpace(name)
	return n == "" || n == HostLabel(rawURL)
}
