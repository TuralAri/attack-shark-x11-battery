package hid

import "fmt"

const (
	VendorID       uint16 = 0x1D57
	UsagePageEvent uint16 = 0x000A

	ReportIDStatus byte = 0x03
	CmdBatteryA    byte = 0x40
	CmdBatteryB    byte = 0x41
)

// SupportedPIDs lists the Attack Shark X11 USB Product IDs (2.4 GHz dongle and wired).
var SupportedPIDs = []uint16{
	0xFA60, // 2.4 GHz wireless dongle / receiver
	0xFA55, // Wired USB mode
	0x22A9, // Wired USB alternate revision
}

// ChargeState describes the mouse power/charging state reported in byte 3.
type ChargeState uint8

const (
	StateUnknown     ChargeState = 0xFF
	StateSleeping    ChargeState = 0x00
	StateDischarging ChargeState = 0x01
	StateFull        ChargeState = 0x02
	StateCharging    ChargeState = 0x03
)

func (s ChargeState) String() string {
	switch s {
	case StateDischarging:
		return "Sur batterie"
	case StateCharging:
		return "En charge"
	case StateFull:
		return "Chargée"
	case StateSleeping:
		return "En veille"
	default:
		return "Inconnu"
	}
}

// BatteryReading represents a decoded battery status report from the mouse.
type BatteryReading struct {
	Percentage int
	State      ChargeState
	DeviceID   byte
	Raw        [5]byte
}

// ParseReport decodes a 5-byte HID input report from Usage Page 0x000A.
//
// Expected packet format:
//
//	[0x03, <device_id>, 0x40|0x41, <state>, <percentage 0..100>]
func ParseReport(data []byte) (BatteryReading, bool) {
	if len(data) < 5 {
		return BatteryReading{}, false
	}
	if data[0] != ReportIDStatus {
		return BatteryReading{}, false
	}
	if data[2] != CmdBatteryA && data[2] != CmdBatteryB {
		return BatteryReading{}, false
	}

	pct := int(data[4])
	if pct > 100 {
		return BatteryReading{}, false
	}

	var state ChargeState
	switch data[3] {
	case 0x00:
		state = StateSleeping
	case 0x01:
		state = StateDischarging
	case 0x02:
		state = StateFull
	case 0x03:
		state = StateCharging
	default:
		state = StateUnknown
	}

	var raw [5]byte
	copy(raw[:], data[:5])

	return BatteryReading{
		Percentage: pct,
		State:      state,
		DeviceID:   data[1],
		Raw:        raw,
	}, true
}

func (r BatteryReading) FormatRaw() string {
	return fmt.Sprintf("%02X-%02X-%02X-%02X-%02X", r.Raw[0], r.Raw[1], r.Raw[2], r.Raw[3], r.Raw[4])
}

// IsSupportedPID checks whether a USB Product ID belongs to an Attack Shark X11.
func IsSupportedPID(pid uint16) bool {
	for _, p := range SupportedPIDs {
		if p == pid {
			return true
		}
	}
	return false
}
