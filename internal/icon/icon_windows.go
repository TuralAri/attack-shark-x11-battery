//go:build windows

package icon

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	modUser32 = syscall.NewLazyDLL("user32.dll")
	modGdi32  = syscall.NewLazyDLL("gdi32.dll")

	procCreateIconIndirect = modUser32.NewProc("CreateIconIndirect")
	procDestroyIcon        = modUser32.NewProc("DestroyIcon")

	procCreateDIBSection = modGdi32.NewProc("CreateDIBSection")
	procCreateBitmap     = modGdi32.NewProc("CreateBitmap")
	procDeleteObject     = modGdi32.NewProc("DeleteObject")
)

const iconSize = 32

type bitmapInfoHeader struct {
	biSize          uint32
	biWidth         int32
	biHeight        int32
	biPlanes        uint16
	biBitCount      uint16
	biCompression   uint32
	biSizeImage     uint32
	biXPelsPerMeter int32
	biYPelsPerMeter int32
	biClrUsed       uint32
	biClrImportant  uint32
}

type bitmapInfo struct {
	bmiHeader bitmapInfoHeader
	bmiColors [1]uint32
}

type iconInfo struct {
	fIcon    int32
	xHotspot uint32
	yHotspot uint32
	hbmMask  uintptr
	hbmColor uintptr
}

// RGBA color.
type rgba struct{ r, g, b, a byte }

// VisualState defines how the tray icon should be rendered.
type VisualState int

const (
	VisualDisconnected VisualState = iota
	VisualDischarging
	VisualCharging
	VisualFull
	VisualSleeping
)

// Destroy frees a Win32 HICON handle.
func Destroy(hicon uintptr) {
	if hicon != 0 {
		procDestroyIcon.Call(hicon)
	}
}

// IsLightTaskbar detects whether Windows uses the light taskbar theme.
func IsLightTaskbar() bool {
	path, err := syscall.UTF16PtrFromString(`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`)
	if err != nil {
		return false
	}
	var hKey syscall.Handle
	if err := syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, path, 0, syscall.KEY_READ, &hKey); err != nil {
		return false
	}
	defer syscall.RegCloseKey(hKey)

	valName, _ := syscall.UTF16PtrFromString("SystemUsesLightTheme")
	var typ uint32
	var val uint32
	size := uint32(unsafe.Sizeof(val))
	err = syscall.RegQueryValueEx(hKey, valName, nil, &typ, (*byte)(unsafe.Pointer(&val)), &size)
	if err != nil {
		return false
	}
	return val == 1
}

// Render generates a crisp 32x32 Win32 HICON showing the battery percentage and color bar.
func Render(percentage int, state VisualState) (uintptr, error) {
	pixels := make([]byte, iconSize*iconSize*4) // BGRA premultiplied

	light := IsLightTaskbar()
	fg := rgba{255, 255, 255, 255}
	track := rgba{70, 75, 85, 210}
	if light {
		fg = rgba{24, 28, 36, 255}
		track = rgba{190, 195, 205, 230}
	}

	barColor := rgba{46, 204, 113, 255} // Green
	switch state {
	case VisualDisconnected:
		barColor = rgba{135, 142, 155, 255}
	case VisualSleeping:
		barColor = rgba{148, 163, 184, 255}
	case VisualCharging:
		barColor = rgba{56, 189, 248, 255} // Cyan
	case VisualFull:
		barColor = rgba{34, 197, 94, 255}
	default:
		if percentage <= 20 {
			barColor = rgba{239, 68, 68, 255} // Red
		} else if percentage <= 40 {
			barColor = rgba{245, 158, 11, 255} // Amber
		}
	}

	// Text label: "--" if disconnected, otherwise numeric percentage ("0".."100").
	label := "--"
	if state != VisualDisconnected && percentage >= 0 {
		if percentage > 100 {
			percentage = 100
		}
		label = fmt.Sprintf("%d", percentage)
	}

	// Draw crisp centered digits in the upper area (y = 2..20).
	if len(label) == 3 {
		// Compact 3-digit layout for "100" using 5x7 glyphs scaled 2x vertically / 1.5x-style (8px wide each + 1px gap = 26px).
		drawTextCompact(pixels, label, 3, 4, fg)
	} else {
		// Bold 2x scaled 5x7 font (each char 10x14, gap 2px).
		charW := 10
		gap := 2
		totalW := len(label)*charW + (len(label)-1)*gap
		startX := (iconSize - totalW) / 2
		drawTextScaled(pixels, label, startX, 3, 2, fg)
	}

	// Draw sleek battery bar at the bottom (y = 23..29, x = 2..29).
	const barX, barY, barW, barH = 2, 23, 28, 6
	fillRect(pixels, barX, barY, barW, barH, track)

	if state != VisualDisconnected && percentage >= 0 {
		fillW := (barW * percentage) / 100
		if percentage > 0 && fillW < 2 {
			fillW = 2
		}
		if fillW > barW {
			fillW = barW
		}
		fillRect(pixels, barX, barY, fillW, barH, barColor)
	}

	// Small charging indicator dot in top-right when charging.
	if state == VisualCharging {
		fillRect(pixels, 27, 1, 4, 4, rgba{56, 189, 248, 255})
	}

	return createIconFromBGRA(pixels, iconSize, iconSize)
}

// 5x7 pixel font for digits and '-'.
var font5x7 = map[rune][7]uint8{
	'0': {0b01110, 0b10001, 0b10011, 0b10101, 0b11001, 0b10001, 0b01110},
	'1': {0b00100, 0b01100, 0b00100, 0b00100, 0b00100, 0b00100, 0b01110},
	'2': {0b01110, 0b10001, 0b00001, 0b00110, 0b01000, 0b10000, 0b11111},
	'3': {0b01110, 0b10001, 0b00001, 0b00110, 0b00001, 0b10001, 0b01110},
	'4': {0b00010, 0b00110, 0b01010, 0b10010, 0b11111, 0b00010, 0b00010},
	'5': {0b11111, 0b10000, 0b11110, 0b00001, 0b00001, 0b10001, 0b01110},
	'6': {0b01110, 0b10000, 0b11110, 0b10001, 0b10001, 0b10001, 0b01110},
	'7': {0b11111, 0b00001, 0b00010, 0b00100, 0b01000, 0b01000, 0b01000},
	'8': {0b01110, 0b10001, 0b10001, 0b01110, 0b10001, 0b10001, 0b01110},
	'9': {0b01110, 0b10001, 0b10001, 0b01111, 0b00001, 0b00010, 0b01100},
	'-': {0b00000, 0b00000, 0b00000, 0b11111, 0b00000, 0b00000, 0b00000},
}

// Compact 4x7 glyphs for 3-digit "100".
var font4x7 = map[rune][7]uint8{
	'1': {0b0100, 0b1100, 0b0100, 0b0100, 0b0100, 0b0100, 0b1110},
	'0': {0b0110, 0b1001, 0b1001, 0b1001, 0b1001, 0b1001, 0b0110},
}

func drawTextScaled(buf []byte, text string, x, y, scale int, c rgba) {
	cx := x
	for _, ch := range text {
		glyph, ok := font5x7[ch]
		if !ok {
			cx += 5*scale + 2
			continue
		}
		for row := 0; row < 7; row++ {
			bits := glyph[row]
			for col := 0; col < 5; col++ {
				if (bits>>(4-col))&1 != 0 {
					fillRect(buf, cx+col*scale, y+row*scale, scale, scale, c)
				}
			}
		}
		cx += 5*scale + 2
	}
}

func drawTextCompact(buf []byte, text string, x, y int, c rgba) {
	cx := x
	for _, ch := range text {
		glyph, ok := font4x7[ch]
		if !ok {
			cx += 9
			continue
		}
		for row := 0; row < 7; row++ {
			bits := glyph[row]
			for col := 0; col < 4; col++ {
				if (bits>>(3-col))&1 != 0 {
					fillRect(buf, cx+col*2, y+row*2, 2, 2, c)
				}
			}
		}
		cx += 9
	}
}

func fillRect(buf []byte, x, y, w, h int, c rgba) {
	for dy := 0; dy < h; dy++ {
		py := y + dy
		if py < 0 || py >= iconSize {
			continue
		}
		for dx := 0; dx < w; dx++ {
			px := x + dx
			if px < 0 || px >= iconSize {
				continue
			}
			idx := (py*iconSize + px) * 4
			buf[idx+0] = c.b
			buf[idx+1] = c.g
			buf[idx+2] = c.r
			buf[idx+3] = c.a
		}
	}
}

func createIconFromBGRA(pixels []byte, w, h int) (uintptr, error) {
	var bi bitmapInfo
	bi.bmiHeader.biSize = uint32(unsafe.Sizeof(bi.bmiHeader))
	bi.bmiHeader.biWidth = int32(w)
	bi.bmiHeader.biHeight = -int32(h) // top-down DIB
	bi.bmiHeader.biPlanes = 1
	bi.bmiHeader.biBitCount = 32

	var bitsPtr unsafe.Pointer
	hbmColor, _, err := procCreateDIBSection.Call(
		0,
		uintptr(unsafe.Pointer(&bi)),
		0, // DIB_RGB_COLORS
		uintptr(unsafe.Pointer(&bitsPtr)),
		0,
		0,
	)
	if hbmColor == 0 || bitsPtr == nil {
		return 0, fmt.Errorf("CreateDIBSection: %w", err)
	}
	defer procDeleteObject.Call(hbmColor)

	dst := unsafe.Slice((*byte)(bitsPtr), len(pixels))
	copy(dst, pixels)

	// Empty 1-bit mask bitmap (alpha channel in hbmColor controls transparency).
	maskBytes := make([]byte, (w*h)/8)
	hbmMask, _, err := procCreateBitmap.Call(
		uintptr(w),
		uintptr(h),
		1,
		1,
		uintptr(unsafe.Pointer(&maskBytes[0])),
	)
	if hbmMask == 0 {
		return 0, fmt.Errorf("CreateBitmap: %w", err)
	}
	defer procDeleteObject.Call(hbmMask)

	ii := iconInfo{
		fIcon:    1,
		hbmMask:  hbmMask,
		hbmColor: hbmColor,
	}
	hicon, _, err := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	if hicon == 0 {
		return 0, fmt.Errorf("CreateIconIndirect: %w", err)
	}
	return hicon, nil
}
