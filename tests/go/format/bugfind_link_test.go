package format_test

import (
	. "eagle/internal/format"

	"testing"

	"eagle/internal/setting"
)

// A trojan / vless link whose userinfo carries BOTH a user name and a password
// ("trojan://user:secret@host:443") must import the password — the credential the
// node dials with — not the user name.
//
// The parser keeps the user name in "pass" unconditionally, even though the file's own
// comment says "url.password() when present", and even though the two sibling parsers
// in this same file (parseAnyTLS, parseSnell) do take the password when it is there.
// The result is a node that is listed, healthy-looking, and can never connect: the
// credential the panel gave is on the other side of the colon.
func TestBugTrojanVLESSUsesUserinfoPassword(t *testing.T) {
	s := &setting.Setting{}
	cases := []struct {
		link string
		typ  string
	}{
		{"trojan://alice:s3cr3t@example.com:443#node", "trojan"},
		{"vless://alice:s3cr3t@example.com:443#node", "vless"},
	}
	for _, c := range cases {
		typ, b, ok := ParseLink(c.link, s)
		if !ok {
			t.Fatalf("ParseLink(%q) failed", c.link)
		}
		if typ != c.typ {
			t.Fatalf("ParseLink(%q) type = %q, want %q", c.link, typ, c.typ)
		}
		if got := b.Str("pass"); got != "s3cr3t" {
			t.Errorf("ParseLink(%q) pass = %q, want %q (the password after the colon)",
				c.link, got, "s3cr3t")
		}
	}
}

// The same link written the ordinary way (the password alone in the userinfo) must
// keep working — the fix is to prefer the password only when there is one.
func TestBugTrojanVLESSPlainUserinfoStillWorks(t *testing.T) {
	s := &setting.Setting{}
	_, b, ok := ParseLink("trojan://onlypassword@example.com:443#node", s)
	if !ok {
		t.Fatal("ParseLink failed on an ordinary trojan link")
	}
	if got := b.Str("pass"); got != "onlypassword" {
		t.Errorf("pass = %q, want %q", got, "onlypassword")
	}
}
