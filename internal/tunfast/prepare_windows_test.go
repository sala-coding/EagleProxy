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
		t.Fatalf("CreateAdapter on an existing adapter returned no error")
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
