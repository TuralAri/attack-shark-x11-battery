//go:build windows

package hid

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

var (
	modHid      = syscall.NewLazyDLL("hid.dll")
	modSetupAPI = syscall.NewLazyDLL("setupapi.dll")
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")

	procHidDGetHidGuid      = modHid.NewProc("HidD_GetHidGuid")
	procHidDGetAttributes   = modHid.NewProc("HidD_GetAttributes")
	procHidDGetPreparsed    = modHid.NewProc("HidD_GetPreparsedData")
	procHidDFreePreparsed   = modHid.NewProc("HidD_FreePreparsedData")
	procHidPGetCaps         = modHid.NewProc("HidP_GetCaps")

	procSetupDiGetClassDevsW             = modSetupAPI.NewProc("SetupDiGetClassDevsW")
	procSetupDiEnumDeviceInterfaces      = modSetupAPI.NewProc("SetupDiEnumDeviceInterfaces")
	procSetupDiGetDeviceInterfaceDetailW = modSetupAPI.NewProc("SetupDiGetDeviceInterfaceDetailW")
	procSetupDiDestroyDeviceInfoList     = modSetupAPI.NewProc("SetupDiDestroyDeviceInfoList")

	procCreateEventW        = modKernel32.NewProc("CreateEventW")
	procResetEvent          = modKernel32.NewProc("ResetEvent")
	procGetOverlappedResult = modKernel32.NewProc("GetOverlappedResult")
	procCancelIoEx          = modKernel32.NewProc("CancelIoEx")
)

const (
	digcfPresent         = 0x00000002
	digcfDeviceInterface = 0x00000010

	fileFlagOverlapped = 0x40000000
	errorIOPending     = syscall.Errno(997)
	waitObject0        = 0x00000000
	waitTimeout        = 0x00000102
)

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

type spDeviceInterfaceData struct {
	cbSize             uint32
	interfaceClassGuid guid
	flags              uint32
	reserved           uintptr
}

type hiddAttributes struct {
	Size          uint32
	VendorID      uint16
	ProductID     uint16
	VersionNumber uint16
}

type hidpCaps struct {
	Usage                     uint16
	UsagePage                 uint16
	InputReportByteLength     uint16
	OutputReportByteLength    uint16
	FeatureReportByteLength   uint16
	Reserved                  [17]uint16
	NumberLinkCollectionNodes uint16
	NumberInputButtonCaps     uint16
	NumberInputValueCaps      uint16
	NumberInputDataIndices    uint16
	NumberOutputButtonCaps    uint16
	NumberOutputValueCaps     uint16
	NumberOutputDataIndices   uint16
	NumberFeatureButtonCaps   uint16
	NumberFeatureValueCaps    uint16
	NumberFeatureDataIndices  uint16
}

// DeviceInfo identifies the HID event collection of a connected Attack Shark X11.
type DeviceInfo struct {
	Path      string
	VendorID  uint16
	ProductID uint16
	UsagePage uint16
	InputLen  uint16
}

var ErrDeviceNotFound = errors.New("Attack Shark X11 introuvable")

// FindDevice locates the Attack Shark X11 battery/event HID interface (Usage Page 0x000A).
func FindDevice() (*DeviceInfo, error) {
	var hidGuid guid
	procHidDGetHidGuid.Call(uintptr(unsafe.Pointer(&hidGuid)))

	devInfoSet, _, err := procSetupDiGetClassDevsW.Call(
		uintptr(unsafe.Pointer(&hidGuid)),
		0,
		0,
		digcfPresent|digcfDeviceInterface,
	)
	if devInfoSet == 0 || devInfoSet == uintptr(syscall.InvalidHandle) {
		return nil, fmt.Errorf("SetupDiGetClassDevsW: %w", err)
	}
	defer procSetupDiDestroyDeviceInfoList.Call(devInfoSet)

	var ifData spDeviceInterfaceData
	ifData.cbSize = uint32(unsafe.Sizeof(ifData))

	detailCbSize := uint32(6)
	if unsafe.Sizeof(uintptr(0)) == 8 {
		detailCbSize = 8
	}

	for idx := uint32(0); ; idx++ {
		r, _, _ := procSetupDiEnumDeviceInterfaces.Call(
			devInfoSet,
			0,
			uintptr(unsafe.Pointer(&hidGuid)),
			uintptr(idx),
			uintptr(unsafe.Pointer(&ifData)),
		)
		if r == 0 {
			break
		}

		var required uint32
		procSetupDiGetDeviceInterfaceDetailW.Call(
			devInfoSet,
			uintptr(unsafe.Pointer(&ifData)),
			0,
			0,
			uintptr(unsafe.Pointer(&required)),
			0,
		)
		if required < 8 {
			continue
		}

		buf := make([]byte, required)
		*(*uint32)(unsafe.Pointer(&buf[0])) = detailCbSize

		r, _, _ = procSetupDiGetDeviceInterfaceDetailW.Call(
			devInfoSet,
			uintptr(unsafe.Pointer(&ifData)),
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(required),
			uintptr(unsafe.Pointer(&required)),
			0,
		)
		if r == 0 {
			continue
		}

		path := utf16BytesToString(buf[4:])
		info, ok := inspectDevice(path)
		if ok {
			return info, nil
		}
	}

	return nil, ErrDeviceNotFound
}

func inspectDevice(path string) (*DeviceInfo, bool) {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, false
	}

	h, err := syscall.CreateFile(
		pathPtr,
		0,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
		nil,
		syscall.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil {
		return nil, false
	}
	defer syscall.CloseHandle(h)

	var attr hiddAttributes
	attr.Size = uint32(unsafe.Sizeof(attr))
	r, _, _ := procHidDGetAttributes.Call(uintptr(h), uintptr(unsafe.Pointer(&attr)))
	if r == 0 || attr.VendorID != VendorID || !IsSupportedPID(attr.ProductID) {
		return nil, false
	}

	var ppd uintptr
	r, _, _ = procHidDGetPreparsed.Call(uintptr(h), uintptr(unsafe.Pointer(&ppd)))
	if r == 0 || ppd == 0 {
		return nil, false
	}
	defer procHidDFreePreparsed.Call(ppd)

	var caps hidpCaps
	procHidPGetCaps.Call(ppd, uintptr(unsafe.Pointer(&caps)))
	if caps.UsagePage != UsagePageEvent || caps.InputReportByteLength < 5 {
		return nil, false
	}

	return &DeviceInfo{
		Path:      path,
		VendorID:  attr.VendorID,
		ProductID: attr.ProductID,
		UsagePage: caps.UsagePage,
		InputLen:  caps.InputReportByteLength,
	}, true
}

func utf16BytesToString(b []byte) string {
	n := len(b) / 2
	u16 := make([]uint16, 0, n)
	for i := 0; i+1 < len(b); i += 2 {
		v := uint16(b[i]) | uint16(b[i+1])<<8
		if v == 0 {
			break
		}
		u16 = append(u16, v)
	}
	return syscall.UTF16ToString(u16)
}

// Reader reads asynchronous HID input reports from an opened Attack Shark X11 interface.
type Reader struct {
	info   *DeviceInfo
	handle syscall.Handle
	event  syscall.Handle
}

// Open opens the HID interface in overlapped mode for non-blocking, cancellable reads.
func Open(info *DeviceInfo) (*Reader, error) {
	pathPtr, err := syscall.UTF16PtrFromString(info.Path)
	if err != nil {
		return nil, err
	}

	h, err := syscall.CreateFile(
		pathPtr,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
		nil,
		syscall.OPEN_EXISTING,
		fileFlagOverlapped,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("CreateFile HID: %w", err)
	}

	ev, _, err := procCreateEventW.Call(0, 1, 0, 0) // manual-reset event
	if ev == 0 {
		syscall.CloseHandle(h)
		return nil, fmt.Errorf("CreateEventW: %w", err)
	}

	return &Reader{
		info:   info,
		handle: h,
		event:  syscall.Handle(ev),
	}, nil
}

// Close releases the HID handle and event handle.
func (r *Reader) Close() {
	if r.handle != 0 && r.handle != syscall.InvalidHandle {
		procCancelIoEx.Call(uintptr(r.handle), 0)
		syscall.CloseHandle(r.handle)
		r.handle = syscall.InvalidHandle
	}
	if r.event != 0 && r.event != syscall.InvalidHandle {
		syscall.CloseHandle(r.event)
		r.event = syscall.InvalidHandle
	}
}

// ErrTimeout is returned when no HID packet arrives within the specified timeout.
var ErrTimeout = errors.New("timeout HID")

// ReadPacket waits for a single raw HID input report up to timeout or ctx cancellation.
func (r *Reader) ReadPacket(ctx context.Context, timeout time.Duration) ([]byte, error) {
	buf := make([]byte, r.info.InputLen)
	procResetEvent.Call(uintptr(r.event))

	var ov syscall.Overlapped
	ov.HEvent = r.event

	var read uint32
	err := syscall.ReadFile(r.handle, buf, &read, &ov)
	if err == nil {
		return buf[:read], nil
	}
	if err != errorIOPending {
		return nil, err
	}

	deadline := time.Now().Add(timeout)
	for {
		if ctx.Err() != nil {
			procCancelIoEx.Call(uintptr(r.handle), uintptr(unsafe.Pointer(&ov)))
			return nil, ctx.Err()
		}

		rem := time.Until(deadline)
		if rem <= 0 {
			procCancelIoEx.Call(uintptr(r.handle), uintptr(unsafe.Pointer(&ov)))
			return nil, ErrTimeout
		}

		waitMs := uint32(250)
		if rem < 250*time.Millisecond {
			waitMs = uint32(rem.Milliseconds())
			if waitMs == 0 {
				waitMs = 1
			}
		}

		w, werr := syscall.WaitForSingleObject(r.event, waitMs)
		if w == waitObject0 {
			ok, _, gerr := procGetOverlappedResult.Call(
				uintptr(r.handle),
				uintptr(unsafe.Pointer(&ov)),
				uintptr(unsafe.Pointer(&read)),
				0,
			)
			if ok == 0 {
				return nil, gerr
			}
			return buf[:read], nil
		}
		if w != waitTimeout {
			procCancelIoEx.Call(uintptr(r.handle), uintptr(unsafe.Pointer(&ov)))
			return nil, werr
		}
	}
}
