//go:build windows

package tray

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"attack-shark-x11-battery/internal/autostart"
	"attack-shark-x11-battery/internal/icon"
)

var (
	modUser32   = syscall.NewLazyDLL("user32.dll")
	modShell32  = syscall.NewLazyDLL("shell32.dll")
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")

	procGetModuleHandleW      = modKernel32.NewProc("GetModuleHandleW")
	procRegisterClassExW      = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW       = modUser32.NewProc("CreateWindowExW")
	procDefWindowProcW        = modUser32.NewProc("DefWindowProcW")
	procDestroyWindow         = modUser32.NewProc("DestroyWindow")
	procGetMessageW           = modUser32.NewProc("GetMessageW")
	procTranslateMessage      = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW      = modUser32.NewProc("DispatchMessageW")
	procPostMessageW          = modUser32.NewProc("PostMessageW")
	procPostQuitMessage       = modUser32.NewProc("PostQuitMessage")
	procRegisterWindowMessage = modUser32.NewProc("RegisterWindowMessageW")
	procCreatePopupMenu       = modUser32.NewProc("CreatePopupMenu")
	procAppendMenuW           = modUser32.NewProc("AppendMenuW")
	procDestroyMenu           = modUser32.NewProc("DestroyMenu")
	procTrackPopupMenu        = modUser32.NewProc("TrackPopupMenu")
	procGetCursorPos          = modUser32.NewProc("GetCursorPos")
	procSetForegroundWindow   = modUser32.NewProc("SetForegroundWindow")
	procShellNotifyIconW      = modShell32.NewProc("Shell_NotifyIconW")
)

const (
	wmDestroy     = 0x0002
	wmCommand     = 0x0111
	wmSettingChg  = 0x001A
	wmApp         = 0x8000
	wmTrayIcon    = wmApp + 1
	wmStateUpdate = wmApp + 2
	wmNotifyLow   = wmApp + 3

	wmLButtonUp   = 0x0202
	wmRButtonUp   = 0x0205
	wmContextMenu = 0x007B

	nimAdd    = 0x00000000
	nimModify = 0x00000001
	nimDelete = 0x00000002

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004
	nifInfo    = 0x00000010

	niifWarning = 0x00000002

	mfString    = 0x00000000
	mfGrayed    = 0x00000001
	mfChecked   = 0x00000008
	mfSeparator = 0x00000800

	tpmRightButton = 0x0002
	tpmBottomAlign = 0x0020
	tpmReturnCmd   = 0x0100

	menuIDReconnect = 1001
	menuIDAutostart = 1002
	menuIDQuit      = 1003

	trayIconID = 1
)

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type point struct {
	X int32
	Y int32
}

type msg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type notifyIconDataW struct {
	cbSize            uint32
	hWnd              uintptr
	uID               uint32
	uFlags            uint32
	uCallbackMessage  uint32
	hIcon             uintptr
	szTip             [128]uint16
	dwState           uint32
	dwStateMask       uint32
	szInfo            [256]uint16
	uTimeoutOrVersion uint32
	szInfoTitle       [64]uint16
	dwInfoFlags       uint32
	guidItem          [16]byte
	hBalloonIcon      uintptr
}

// State holds the latest mouse battery snapshot displayed in the tray.
type State struct {
	Connected  bool
	Sleeping   bool
	Percentage int
	StatusText string
	Visual     icon.VisualState
	UpdatedAt  time.Time
}

// App manages the Windows system tray icon and message loop.
type App struct {
	mu               sync.Mutex
	hwnd             uintptr
	currentIcon      uintptr
	state            State
	lowNotifTitle    string
	lowNotifBody     string
	wmTaskbarCreated uint32

	onReconnect func()
	onQuit      func()
}

var activeApp *App

// New creates a new tray App instance.
func New(onReconnect, onQuit func()) *App {
	return &App{
		state: State{
			Connected:  false,
			Percentage: -1,
			StatusText: "Recherche du dongle…",
			Visual:     icon.VisualDisconnected,
		},
		onReconnect: onReconnect,
		onQuit:      onQuit,
	}
}

// UpdateState safely updates the displayed battery state from any goroutine.
func (a *App) UpdateState(st State) {
	a.mu.Lock()
	a.state = st
	hwnd := a.hwnd
	a.mu.Unlock()

	if hwnd != 0 {
		procPostMessageW.Call(hwnd, wmStateUpdate, 0, 0)
	}
}

// NotifyLowBattery displays a Windows tray notification balloon.
func (a *App) NotifyLowBattery(title, body string) {
	a.mu.Lock()
	a.lowNotifTitle = title
	a.lowNotifBody = body
	hwnd := a.hwnd
	a.mu.Unlock()

	if hwnd != 0 {
		procPostMessageW.Call(hwnd, wmNotifyLow, 0, 0)
	}
}

// Run starts the Win32 message loop and blocks until the user exits.
func (a *App) Run() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	activeApp = a

	hInst, _, _ := procGetModuleHandleW.Call(0)
	className, _ := syscall.UTF16PtrFromString("AttackSharkX11BatteryTrayClass")

	tbMsgName, _ := syscall.UTF16PtrFromString("TaskbarCreated")
	tbMsg, _, _ := procRegisterWindowMessage.Call(uintptr(unsafe.Pointer(tbMsgName)))
	a.wmTaskbarCreated = uint32(tbMsg)

	wc := wndClassExW{
		cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		lpfnWndProc:   syscall.NewCallback(wndProc),
		hInstance:     hInst,
		lpszClassName: className,
	}
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("RegisterClassExW: %w", err)
	}

	windowName, _ := syscall.UTF16PtrFromString("Attack Shark X11 Battery")
	hwnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		0,
		0, 0, 0, 0,
		0, 0, hInst, 0,
	)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW: %w", err)
	}

	a.mu.Lock()
	a.hwnd = hwnd
	a.mu.Unlock()

	a.addTrayIcon()
	defer a.removeTrayIcon()

	var m msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	return nil
}

func wndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	app := activeApp
	if app == nil {
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
		return r
	}

	if app.wmTaskbarCreated != 0 && message == app.wmTaskbarCreated {
		app.addTrayIcon()
		return 0
	}

	switch message {
	case wmTrayIcon:
		ev := uint32(lParam & 0xFFFF)
		if ev == wmRButtonUp || ev == wmContextMenu || ev == wmLButtonUp {
			app.showContextMenu(hwnd)
		}
		return 0

	case wmStateUpdate, wmSettingChg:
		app.refreshTrayIcon()
		return 0

	case wmNotifyLow:
		app.showBalloon()
		return 0

	case wmCommand:
		switch uint32(wParam & 0xFFFF) {
		case menuIDReconnect:
			if app.onReconnect != nil {
				app.onReconnect()
			}
		case menuIDAutostart:
			_ = autostart.SetEnabled(!autostart.IsEnabled())
		case menuIDQuit:
			procDestroyWindow.Call(hwnd)
		}
		return 0

	case wmDestroy:
		if app.onQuit != nil {
			app.onQuit()
		}
		procPostQuitMessage.Call(0)
		return 0
	}

	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}

func (a *App) buildTooltip(st State) string {
	if !st.Connected {
		return fmt.Sprintf("Attack Shark X11 — %s", st.StatusText)
	}
	if st.Percentage < 0 {
		return "Attack Shark X11 — En attente du signal…"
	}
	if st.Sleeping {
		return fmt.Sprintf("Attack Shark X11 : %d%% (en veille)", st.Percentage)
	}
	return fmt.Sprintf("Attack Shark X11 : %d%% (%s)", st.Percentage, st.StatusText)
}

func (a *App) addTrayIcon() {
	a.mu.Lock()
	st := a.state
	hwnd := a.hwnd
	oldIcon := a.currentIcon
	a.mu.Unlock()

	hicon, err := icon.Render(st.Percentage, st.Visual)
	if err != nil {
		return
	}

	var nid notifyIconDataW
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	nid.hWnd = hwnd
	nid.uID = trayIconID
	nid.uFlags = nifMessage | nifIcon | nifTip
	nid.uCallbackMessage = wmTrayIcon
	nid.hIcon = hicon
	copyUTF16(nid.szTip[:], a.buildTooltip(st))

	procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))

	a.mu.Lock()
	a.currentIcon = hicon
	a.mu.Unlock()

	if oldIcon != 0 && oldIcon != hicon {
		icon.Destroy(oldIcon)
	}
}

func (a *App) refreshTrayIcon() {
	a.mu.Lock()
	st := a.state
	hwnd := a.hwnd
	oldIcon := a.currentIcon
	a.mu.Unlock()

	if hwnd == 0 {
		return
	}

	hicon, err := icon.Render(st.Percentage, st.Visual)
	if err != nil {
		return
	}

	var nid notifyIconDataW
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	nid.hWnd = hwnd
	nid.uID = trayIconID
	nid.uFlags = nifIcon | nifTip
	nid.hIcon = hicon
	copyUTF16(nid.szTip[:], a.buildTooltip(st))

	r, _, _ := procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
	if r == 0 {
		nid.uFlags = nifMessage | nifIcon | nifTip
		nid.uCallbackMessage = wmTrayIcon
		procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
	}

	a.mu.Lock()
	a.currentIcon = hicon
	a.mu.Unlock()

	if oldIcon != 0 && oldIcon != hicon {
		icon.Destroy(oldIcon)
	}
}

func (a *App) showBalloon() {
	a.mu.Lock()
	hwnd := a.hwnd
	title := a.lowNotifTitle
	body := a.lowNotifBody
	a.mu.Unlock()

	if hwnd == 0 {
		return
	}

	var nid notifyIconDataW
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	nid.hWnd = hwnd
	nid.uID = trayIconID
	nid.uFlags = nifInfo
	nid.dwInfoFlags = niifWarning
	copyUTF16(nid.szInfoTitle[:], title)
	copyUTF16(nid.szInfo[:], body)

	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
}

func (a *App) removeTrayIcon() {
	a.mu.Lock()
	hwnd := a.hwnd
	oldIcon := a.currentIcon
	a.currentIcon = 0
	a.mu.Unlock()

	if hwnd != 0 {
		var nid notifyIconDataW
		nid.cbSize = uint32(unsafe.Sizeof(nid))
		nid.hWnd = hwnd
		nid.uID = trayIconID
		procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	}
	if oldIcon != 0 {
		icon.Destroy(oldIcon)
	}
}

func (a *App) showContextMenu(hwnd uintptr) {
	a.mu.Lock()
	st := a.state
	a.mu.Unlock()

	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}
	defer procDestroyMenu.Call(hMenu)

	header := "Attack Shark X11 — Déconnectée"
	if st.Connected && st.Percentage >= 0 {
		if st.Sleeping {
			header = fmt.Sprintf("Attack Shark X11 : %d%% (en veille)", st.Percentage)
		} else {
			header = fmt.Sprintf("Attack Shark X11 : %d%% (%s)", st.Percentage, st.StatusText)
		}
	} else if st.Connected {
		header = "Attack Shark X11 — En attente…"
	}

	appendMenuItem(hMenu, mfString|mfGrayed, 0, header)
	if !st.UpdatedAt.IsZero() {
		appendMenuItem(hMenu, mfString|mfGrayed, 0, fmt.Sprintf("Dernière lecture : %s", st.UpdatedAt.Format("15:04:05")))
	}
	appendMenuItem(hMenu, mfSeparator, 0, "")
	appendMenuItem(hMenu, mfString, menuIDReconnect, "Reconnecter le dongle")

	autoFlags := uintptr(mfString)
	if autostart.IsEnabled() {
		autoFlags |= mfChecked
	}
	appendMenuItem(hMenu, autoFlags, menuIDAutostart, "Démarrer avec Windows")
	appendMenuItem(hMenu, mfSeparator, 0, "")
	appendMenuItem(hMenu, mfString, menuIDQuit, "Quitter")

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(hwnd)

	cmd, _, _ := procTrackPopupMenu.Call(
		hMenu,
		tpmRightButton|tpmBottomAlign|tpmReturnCmd,
		uintptr(pt.X),
		uintptr(pt.Y),
		0,
		hwnd,
		0,
	)
	if cmd != 0 {
		procPostMessageW.Call(hwnd, wmCommand, cmd, 0)
	}
}

func appendMenuItem(hMenu, flags, id uintptr, label string) {
	if flags == mfSeparator {
		procAppendMenuW.Call(hMenu, mfSeparator, 0, 0)
		return
	}
	ptr, _ := syscall.UTF16PtrFromString(label)
	procAppendMenuW.Call(hMenu, flags, id, uintptr(unsafe.Pointer(ptr)))
}

func copyUTF16(dst []uint16, s string) {
	u, err := syscall.UTF16FromString(s)
	if err != nil {
		return
	}
	maxLen := len(dst) - 1
	if len(u) > maxLen {
		u = u[:maxLen]
	}
	copy(dst, u)
	dst[len(u)] = 0
}
