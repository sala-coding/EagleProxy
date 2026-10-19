//go:build windows

package tunfast

import (
	"errors"
	"os"
	"syscall"
	"testing"

	"eagle/internal/tunfast/internal/wintun"

	"golang.org/x/sys/windows"
)

// TestAdapterExistsAnswer pins down what WintunCreateAdapter returns for an
// adapter that is already there, and that isAdapterAlreadyExists recognizes it.
//
// It is the answer the whole prepare path turns on: mistaken for a failure, the
// create-not-open fallback never runs and a prepared adapter is reported as a
// broken one. Needs an adapter by that name to exist, and the administrator
// token that touching an adapter needs at all.
func TestAdapterExistsAnswer(t *testing.T) {
	name := "EagleTun"
	if _, err := Find(name); err != nil {
		t.Skipf("no adapter named %s to collide with: %v", name, err)
	}
	_, err := wintun.CreateAdapter(name, tunnelType, generateGUIDByDeviceName(name))
	if err == nil {
		// wintun opened the adapter that was already there instead of
		// refusing it, so there is no errno on this machine to read the
		// answer from. The judgment itself is the table test below, which
		// needs no adapter and no privileges.
		t.Skip("wintun opened the existing adapter rather than refusing it: no errno to read here")
	}
	var errno syscall.Errno
	asErrno := errors.As(err, &errno)
	t.Logf("err=%v", err)
	t.Logf("type=%T", err)
	t.Logf("errors.Is(err, os.ErrExist)=%v", errors.Is(err, os.ErrExist))
	t.Logf("errors.As(err, *syscall.Errno)=%v errno=%d (183=%d, 80=%d)",
		asErrno, errno, windows.ERROR_ALREADY_EXISTS, windows.ERROR_FILE_EXISTS)
	t.Logf("isAdapterAlreadyExists=%v", isAdapterAlreadyExists(err))
	if !isAdapterAlreadyExists(err) {
		t.Errorf("isAdapterAlreadyExists(%v) = false, want true", err)
	}
}

// TestIsAdapterAlreadyExists is the judgment itself, without an adapter: which
// errors mean "this is already there". The codes are the ones Windows answers
// with, including the HRESULT-shaped wrapper WintunCreateAdapter wraps them
// in — the shape that has to be unwrapped before the code can be read.
func TestIsAdapterAlreadyExists(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"already exists", syscall.Errno(windows.ERROR_ALREADY_EXISTS), true},
		{"file exists", syscall.Errno(windows.ERROR_FILE_EXISTS), true},
		{"hresult around already exists", syscall.Errno(0x80070000 | windows.ERROR_ALREADY_EXISTS), true},
		{"hresult around file exists", syscall.Errno(0x80070000 | windows.ERROR_FILE_EXISTS), true},
		{"os.ErrExist", os.ErrExist, true},
		{"access denied", syscall.Errno(windows.ERROR_ACCESS_DENIED), false},
		{"file not found", syscall.Errno(windows.ERROR_FILE_NOT_FOUND), false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		if got := isAdapterAlreadyExists(c.err); got != c.want {
			t.Errorf("isAdapterAlreadyExists(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}
