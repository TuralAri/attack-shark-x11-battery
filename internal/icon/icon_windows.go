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
	procGetSystemMetrics   = modUser32.NewProc("GetSystemMetrics")

	procCreateDIBSection = modGdi32.NewProc("CreateDIBSection")
	procCreateBitmap     = modGdi32.NewProc("CreateBitmap")
	procDeleteObject     = modGdi32.NewProc("DeleteObject")
)

const (
	baseSize   = 16
	smCXSmIcon = 49
)

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

// trayIconSize returns the exact native small-icon size for the current DPI
// (e.g. 16 at 100%, 20 at 125%, 24 at 150%, 32 at 200%) so Windows never
// applies blurry downscaling.
func trayIconSize() int {
	r, _, _ := procGetSystemMetrics.Call(smCXSmIcon)
	s := int(r)
	if s < 16 {
		return 16
	}
	if s > 64 {
		return 64
	}
	return s
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

// Render generates a pixel-perfect Win32 HICON at the exact system tray size,
// showing large bold battery digits and a sleek 2px color bar at the bottom.
func Render(percentage int, state VisualState) (uintptr, error) {
	base := make([]byte, baseSize*baseSize*4) // 16x16 logical grid, BGRA

	light := IsLightTaskbar()
	fg := rgba{255, 255, 255, 255}
	track := rgba{72, 78, 88, 220}
	if light {
		fg = rgba{20, 24, 31, 255}
		track = rgba{185, 192, 204, 235}
	}

	barColor := rgba{46, 204, 113, 255} // Green
	switch state {
	case VisualDisconnected:
		barColor = rgba{135, 142, 155, 255}
		if light {
			fg = rgba{110, 116, 128, 255}
		} else {
			fg = rgba{175, 182, 194, 255}
		}
	case VisualSleeping:
		barColor = rgba{148, 163, 184, 255}
	case VisualCharging:
		barColor = rgba{56, 189, 248, 255} // Cyan
		if light {
			fg = rgba{2, 132, 199, 255}
		} else {
			fg = rgba{125, 211, 252, 255}
		}
	case VisualFull:
		barColor = rgba{34, 197, 94, 255}
	default:
		if percentage <= 20 {
			barColor = rgba{239, 68, 68, 255} // Red
		} else if percentage <= 40 {
			barColor = rgba{245, 158, 11, 255} // Amber
		}
	}

	label := "--"
	if state != VisualDisconnected && percentage >= 0 {
		if percentage > 100 {
			percentage = 100
		}
		label = fmt.Sprintf("%d", percentage)
	}

	// Render bold full-height digits on y = 1..11 (11px tall on 16x16 grid).
	switch len(label) {
	case 3:
		// "100" using crisp 3+1+5+1+5 = 15px layout.
		draw100(base, 0, 1, fg)
	case 2:
		if label == "--" {
			fillRect(base, baseSize, 1, 5, 6, 2, fg)
			fillRect(base, baseSize, 9, 5, 6, 2, fg)
		} else {
			// Two bold 7x11 digits with a 2px gap -> fills all 16px cleanly!
			drawGlyph7x11(base, rune(label[0]), 0, 1, fg)
			drawGlyph7x11(base, rune(label[1]), 9, 1, fg)
		}
	case 1:
		drawGlyph7x11(base, rune(label[0]), 4, 1, fg)
	}

	// Sleek 2px battery bar at the very bottom (y = 14..15, full 16px width).
	const barX, barY, barW, barH = 0, 14, 16, 2
	fillRect(base, baseSize, barX, barY, barW, barH, track)

	if state != VisualDisconnected && percentage >= 0 {
		fillW := (barW*percentage + 50) / 100
		if percentage > 0 && fillW < 1 {
			fillW = 1
		}
		if fillW > barW {
			fillW = barW
		}
		fillRect(base, baseSize, barX, barY, fillW, barH, barColor)
	}

	target := trayIconSize()
	pixels := base
	if target != baseSize {
		pixels = scaleNearest(base, baseSize, target)
	}

	return createIconFromBGRA(pixels, target, target)
}

// Bold 7x11 pixel-art digits (2px vertical stems, crisp 1px horizontals, rounded corners).
// Designed specifically for 1:1 legibility at 16x16 native taskbar resolution.
var font7x11 = map[rune][11]uint8{
	'0': {
		0b0111110,
		0b1100011,
		0b1100011,
		0b1100011,
		0b1100011,
		0b1100011,
		0b1100011,
		0b1100011,
		0b1100011,
		0b1100011,
		0b0111110,
	},
	'1': {
		0b0001100,
		0b0011100,
		0b0111100,
		0b0001100,
		0b0001100,
		0b0001100,
		0b0001100,
		0b0001100,
		0b0001100,
		0b0001100,
		0b0111110,
	},
	'2': {
		0b0111110,
		0b1100011,
		0b0000011,
		0b0000011,
		0b0000110,
		0b0011100,
		0b0110000,
		0b1100000,
		0b1100000,
		0b1100000,
		0b1111111,
	},
	'3': {
		0b0111110,
		0b1100011,
		0b0000011,
		0b0000011,
		0b0000011,
		0b0011110,
		0b0000011,
		0b0000011,
		0b0000011,
		0b1100011,
		0b0111110,
	},
	'4': {
		0b0000110,
		0b0001110,
		0b0011110,
		0b0110110,
		0b1100110,
		0b1100110,
		0b1111111,
		0b0000110,
		0b0000110,
		0b0000110,
		0b0000110,
	},
	'5': {
		0b1111111,
		0b1100000,
		0b1100000,
		0b1100000,
		0b1100000,
		0b1111110,
		0b0000011,
		0b0000011,
		0b0000011,
		0b1100011,
		0b0111110,
	},
	'6': {
		0b0111110,
		0b1100011,
		0b1100000,
		0b1100000,
		0b1100000,
		0b1111110,
		0b1100011,
		0b1100011,
		0b1100011,
		0b1100011,
		0b0111110,
	},
	'7': {
		0b1111111,
		0b0000011,
		0b0000011,
		0b0000110,
		0b0000110,
		0b0001100,
		0b0001100,
		0b0011000,
		0b0011000,
		0b0011000,
		0b0011000,
	},
	'8': {
		0b0111110,
		0b1100011,
		0b1100011,
		0b1100011,
		0b1100011,
		0b0111110,
		0b1100011,
		0b1100011,
		0b1100011,
		0b1100011,
		0b0111110,
	},
	'9': {
		0b0111110,
		0b1100011,
		0b1100011,
		0b1100011,
		0b1100011,
		0b0111111,
		0b0000011,
		0b0000011,
		0b0000011,
		0b1100011,
		0b0111110,
	},
}

var glyph1Slim = [11]uint8{
	0b010,
	0b110,
	0b010,
	0b010,
	0b010,
	0b010,
	0b010,
	0b010,
	0b010,
	0b010,
	0b111,
}

var glyph0Slim = [11]uint8{
	0b01110,
	0b10001,
	0b10001,
	0b10001,
	0b10001,
	0b10001,
	0b10001,
	0b10001,
	0b10001,
	0b10001,
	0b01110,
}

func drawGlyph7x11(buf []byte, ch rune, x, y int, c rgba) {
	glyph, ok := font7x11[ch]
	if !ok {
		return
	}
	for row := 0; row < 11; row++ {
		bits := glyph[row]
		for col := 0; col < 7; col++ {
			if (bits>>(6-col))&1 != 0 {
				setPixel(buf, baseSize, x+col, y+row, c)
			}
		}
	}
}

func draw100(buf []byte, x, y int, c rgba) {
	for row := 0; row < 11; row++ {
		for col := 0; col < 3; col++ {
			if (glyph1Slim[row]>>(2-col))&1 != 0 {
				setPixel(buf, baseSize, x+col, y+row, c)
			}
		}
		for col := 0; col < 5; col++ {
			if (glyph0Slim[row]>>(4-col))&1 != 0 {
				setPixel(buf, baseSize, x+4+col, y+row, c)
				setPixel(buf, baseSize, x+10+col, y+row, c)
			}
		}
	}
}

func setPixel(buf []byte, stride, x, y int, c rgba) {
	if x < 0 || x >= stride || y < 0 || y >= stride {
		return
	}
	idx := (y*stride + x) * 4
	buf[idx+0] = c.b
	buf[idx+1] = c.g
	buf[idx+2] = c.r
	buf[idx+3] = c.a
}

func fillRect(buf []byte, stride, x, y, w, h int, c rgba) {
	for dy := 0; dy < h; dy++ {
		for dx := 0; dx < w; dx++ {
			setPixel(buf, stride, x+dx, y+dy, c)
		}
	}
}

func scaleNearest(src []byte, srcSize, dstSize int) []byte {
	dst := make([]byte, dstSize*dstSize*4)
	for dy := 0; dy < dstSize; dy++ {
		sy := (dy * srcSize) / dstSize
		for dx := 0; dx < dstSize; dx++ {
			sx := (dx * srcSize) / dstSize
			sIdx := (sy*srcSize + sx) * 4
			dIdx := (dy*dstSize + dx) * 4
			copy(dst[dIdx:dIdx+4], src[sIdx:sIdx+4])
		}
	}
	return dst
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

	// 1-bit mask bitmap (stride must be padded to 16-bit WORD boundary per Win32 CreateBitmap).
	rowBytes := ((w + 15) / 16) * 2
	maskBytes := make([]byte, rowBytes*h)
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
