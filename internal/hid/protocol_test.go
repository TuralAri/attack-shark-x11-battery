package hid

import "testing"

func TestParseReport(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		wantOK   bool
		wantPct  int
		wantStat ChargeState
	}{
		{
			name:     "live dongle discharging 92%",
			input:    []byte{0x03, 0x07, 0x40, 0x01, 0x5C},
			wantOK:   true,
			wantPct:  92,
			wantStat: StateDischarging,
		},
		{
			name:     "charging with cmd 0x41",
			input:    []byte{0x03, 0x55, 0x41, 0x03, 0x32},
			wantOK:   true,
			wantPct:  50,
			wantStat: StateCharging,
		},
		{
			name:     "fully charged 100%",
			input:    []byte{0x03, 0x07, 0x40, 0x02, 0x64},
			wantOK:   true,
			wantPct:  100,
			wantStat: StateFull,
		},
		{
			name:     "sleeping state",
			input:    []byte{0x03, 0x07, 0x40, 0x00, 0x40},
			wantOK:   true,
			wantPct:  64,
			wantStat: StateSleeping,
		},
		{
			name:   "wrong report id",
			input:  []byte{0x02, 0x07, 0x40, 0x01, 0x5C},
			wantOK: false,
		},
		{
			name:   "non-battery command",
			input:  []byte{0x03, 0x07, 0x10, 0x01, 0x5C},
			wantOK: false,
		},
		{
			name:   "out of range percentage",
			input:  []byte{0x03, 0x07, 0x40, 0x01, 0xFF},
			wantOK: false,
		},
		{
			name:   "short buffer",
			input:  []byte{0x03, 0x07, 0x40},
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseReport(tc.input)
			if ok != tc.wantOK {
				t.Fatalf("ParseReport(%X) ok = %v, want %v", tc.input, ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if got.Percentage != tc.wantPct {
				t.Errorf("Percentage = %d, want %d", got.Percentage, tc.wantPct)
			}
			if got.State != tc.wantStat {
				t.Errorf("State = %v, want %v", got.State, tc.wantStat)
			}
		})
	}
}
