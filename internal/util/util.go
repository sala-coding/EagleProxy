package util

import (
	"encoding/base64"
	"net"
	"strings"
)

// StripANSI removes ANSI escape sequences from a line of process output.
//
// The core colours its log lines when it thinks a terminal is attached, and the
// escapes then reach the UI as literal text: the CSI bytes are invisible and
// what follows them ("[0;32m") reads as noise, so a copied log line carries a
// stray control character. Dropping the whole sequence keeps the text and stops
// the panel from showing escape fragments.
func StripANSI(s string) string {
	if !strings.ContainsRune(s, ansiESC) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != ansiESC {
			b.WriteByte(s[i])
			i++
			continue
		}
		// ESC [ ... <final byte in @..~>  — CSI, the only form the core emits
		if i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] >= 0x20 && s[j] < 0x40 {
				j++
			}
			if j < len(s) {
				j++ // the final byte
			}
			i = j
			continue
		}
		// ESC ] ... BEL/ST is OSC (window title): skip to the terminator.
		if i+1 < len(s) && s[i+1] == ']' {
			j := i + 2
			for j < len(s) && s[j] != '\a' {
				if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
					j++
					break
				}
				j++
			}
			i = j + 1
			continue
		}
		i += 2 // a two-character escape we do not model
	}
	return b.String()
}

const ansiESC = 0x1b

// SplitLinesSkipSharp splits text by newlines, trims each line, drops empty
// lines and lines starting with '#'. Mirrors nekoray SplitLinesSkipSharp.
func SplitLinesSkipSharp(s string, maxLine int) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
		if maxLine > 0 && len(out) >= maxLine {
			break
		}
	}
	return out
}

// IsIPAddress reports whether s is a valid IPv4/IPv6 address.
func IsIPAddress(s string) bool {
	return net.ParseIP(strings.Trim(s, "[]")) != nil
}

// WrapIPv6Host wraps an IPv6 address in brackets.
func WrapIPv6Host(s string) string {
	if ip := net.ParseIP(strings.Trim(s, "[]")); ip != nil && strings.Contains(s, ":") {
		return "[" + strings.Trim(s, "[]") + "]"
	}
	return s
}

// IsValidPort reports whether port is in [1, 65535].
func IsValidPort(port int) bool {
	return port >= 1 && port <= 65535
}

// DecodeB64 tries the common base64 variants (std/url, padded/raw).
func DecodeB64(input string) ([]byte, bool) {
	input = strings.TrimSpace(input)
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, enc := range encodings {
		if b, err := enc.DecodeString(input); err == nil {
			return b, true
		}
	}
	return nil, false
}

// MergeJSON merges src into dst following nekoray's MergeJson semantics:
//   - both objects  -> recursive merge
//   - key "+name"   -> prepend array to dst["name"]
//   - key "name+"   -> append array to dst["name"]
//   - otherwise     -> overwrite
func MergeJSON(dst map[string]interface{}, src map[string]interface{}) {
	if len(src) == 0 {
		return
	}
	for key, vSrc := range src {
		if vDst, ok := dst[key]; ok {
			srcObj, srcIsObj := vSrc.(map[string]interface{})
			dstObj, dstIsObj := vDst.(map[string]interface{})
			if srcIsObj && dstIsObj {
				MergeJSON(dstObj, srcObj)
				dst[key] = dstObj
				continue
			}
			dst[key] = vSrc
			continue
		}
		if arr, isArr := vSrc.([]interface{}); isArr {
			if strings.HasPrefix(key, "+") {
				k2 := strings.TrimPrefix(key, "+")
				dst[k2] = append(cloneSlice(arr), toSlice(dst[k2])...)
				continue
			}
			if strings.HasSuffix(key, "+") {
				k2 := strings.TrimSuffix(key, "+")
				dst[k2] = append(toSlice(dst[k2]), cloneSlice(arr)...)
				continue
			}
		}
		dst[key] = vSrc
	}
}

func toSlice(v interface{}) []interface{} {
	if s, ok := v.([]interface{}); ok {
		return s
	}
	return nil
}

func cloneSlice(s []interface{}) []interface{} {
	out := make([]interface{}, len(s))
	copy(out, s)
	return out
}
