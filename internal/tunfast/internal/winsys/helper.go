//go:build windows

package winsys

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// must panics on an error that cannot be handled by the caller: UTF-16
// conversion of a plain string can only fail on a NUL byte, which neither an
// executable path nor a description we assemble ever contains.
func must(err error) {
	if err != nil {
		panic(err)
	}
}

func CreateDisplayData(name, description string) FWPM_DISPLAY_DATA0 {
	namePtr, err := windows.UTF16PtrFromString(name)
	must(err)

	descriptionPtr, err := windows.UTF16PtrFromString(description)
	must(err)

	return FWPM_DISPLAY_DATA0{
		Name:        namePtr,
		Description: descriptionPtr,
	}
}

func GetCurrentProcessAppID() (*FWP_BYTE_BLOB, error) {
	currentFile, err := os.Executable()
	if err != nil {
		return nil, err
	}

	curFilePtr, err := windows.UTF16PtrFromString(currentFile)
	if err != nil {
		return nil, err
	}

	windows.GetCurrentProcessId()

	var appID *FWP_BYTE_BLOB
	err = FwpmGetAppIdFromFileName0(curFilePtr, unsafe.Pointer(&appID))
	if err != nil {
		return nil, err
	}
	return appID, nil
}
