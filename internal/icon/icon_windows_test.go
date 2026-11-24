//go:build windows

package icon

import "testing"

func TestRenderAllStates(t *testing.T) {
	cases := []struct {
		name  string
		pct   int
		state VisualState
	}{
		{"disconnected", -1, VisualDisconnected},
		{"low_15", 15, VisualDischarging},
		{"medium_35", 35, VisualDischarging},
		{"high_92", 92, VisualDischarging},
		{"full_100", 100, VisualFull},
		{"charging_55", 55, VisualCharging},
		{"sleeping_80", 80, VisualSleeping},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hicon, err := Render(tc.pct, tc.state)
			if err != nil {
				t.Fatalf("Render(%d, %v) failed: %v", tc.pct, tc.state, err)
			}
			if hicon == 0 {
				t.Fatalf("Render(%d, %v) returned null HICON", tc.pct, tc.state)
			}
			Destroy(hicon)
		})
	}
}
