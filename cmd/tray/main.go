//go:build windows

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"

	"attack-shark-x11-battery/internal/hid"
	"attack-shark-x11-battery/internal/icon"
	"attack-shark-x11-battery/internal/tray"
)

const (
	lowBatteryThreshold = 15
	lowBatteryReset     = 25
	sleepTimeout        = 30 * time.Second
	readTimeout         = 5 * time.Second
	retryInterval       = 3 * time.Second
)

var (
	modKernel32      = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutexW = modKernel32.NewProc("CreateMutexW")
)

const errorAlreadyExists = syscall.Errno(183)

func main() {
	once := flag.Bool("once", false, "Lit un paquet de batterie dans le terminal puis quitte")
	raw := flag.Bool("raw", false, "Affiche les paquets HID bruts en continu dans le terminal")
	flag.Parse()

	if *once || *raw {
		runCLI(*once)
		return
	}

	// Single-instance guard for the tray application.
	mutex, ok := acquireSingleInstanceMutex()
	if !ok {
		return
	}
	defer syscall.CloseHandle(mutex)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reconnectCh := make(chan struct{}, 1)
	app := tray.New(
		func() {
			select {
			case reconnectCh <- struct{}{}:
			default:
			}
		},
		cancel,
	)

	go monitorLoop(ctx, app, reconnectCh)

	if err := app.Run(); err != nil {
		os.Exit(1)
	}
}

func acquireSingleInstanceMutex() (syscall.Handle, bool) {
	name, _ := syscall.UTF16PtrFromString(`Local\AttackSharkX11BatteryTray`)
	h, _, err := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return 0, false
	}
	if err == errorAlreadyExists {
		syscall.CloseHandle(syscall.Handle(h))
		return 0, false
	}
	return syscall.Handle(h), true
}

func runCLI(once bool) {
	info, err := hid.FindDevice()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erreur : %v\n", err)
		os.Exit(1)
	}

	reader, err := hid.Open(info)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erreur d'ouverture HID : %v\n", err)
		os.Exit(1)
	}
	defer reader.Close()

	fmt.Printf("Attack Shark X11 détectée (VID=0x%04X PID=0x%04X)\n", info.VendorID, info.ProductID)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	if !once {
		cancel()
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	for {
		pkt, err := reader.ReadPacket(ctx, readTimeout)
		if err != nil {
			if errors.Is(err, hid.ErrTimeout) {
				if once && ctx.Err() != nil {
					fmt.Fprintln(os.Stderr, "Aucun paquet batterie reçu (la souris est peut-être en veille — bougez-la et réessayez).")
					os.Exit(1)
				}
				continue
			}
			fmt.Fprintf(os.Stderr, "Erreur de lecture : %v\n", err)
			os.Exit(1)
		}

		if reading, ok := hid.ParseReport(pkt); ok {
			fmt.Printf("[%s] Batterie : %d%% — %s (brut: %s)\n",
				time.Now().Format("15:04:05"),
				reading.Percentage,
				reading.State.String(),
				reading.FormatRaw(),
			)
			if once {
				return
			}
		} else if !once {
			fmt.Printf("[%s] Paquet ignoré : %X\n", time.Now().Format("15:04:05"), pkt)
		}
	}
}

func monitorLoop(ctx context.Context, app *tray.App, reconnectCh <-chan struct{}) {
	var (
		lastPct      = -1
		lastState    = hid.StateUnknown
		lastPacketAt time.Time
		lowNotified  = false
	)

	for {
		if ctx.Err() != nil {
			return
		}

		info, err := hid.FindDevice()
		if err != nil {
			app.UpdateState(tray.State{
				Connected:  false,
				Percentage: -1,
				StatusText: "Dongle non détecté",
				Visual:     icon.VisualDisconnected,
			})
			if !waitOrCancel(ctx, retryInterval, reconnectCh) {
				return
			}
			continue
		}

		reader, err := hid.Open(info)
		if err != nil {
			app.UpdateState(tray.State{
				Connected:  false,
				Percentage: -1,
				StatusText: "Erreur d'accès HID",
				Visual:     icon.VisualDisconnected,
			})
			if !waitOrCancel(ctx, retryInterval, reconnectCh) {
				return
			}
			continue
		}

		// Connected: show waiting state until first battery packet arrives.
		if lastPct < 0 {
			app.UpdateState(tray.State{
				Connected:  true,
				Percentage: -1,
				StatusText: "En attente du signal…",
				Visual:     icon.VisualSleeping,
			})
		}

		for {
			select {
			case <-ctx.Done():
				reader.Close()
				return
			case <-reconnectCh:
				reader.Close()
				goto reconnect
			default:
			}

			pkt, err := reader.ReadPacket(ctx, readTimeout)
			if err != nil {
				if errors.Is(err, hid.ErrTimeout) {
					// If no heartbeat for sleepTimeout, keep showing last known percentage as sleeping.
					if lastPct >= 0 && time.Since(lastPacketAt) >= sleepTimeout {
						app.UpdateState(tray.State{
							Connected:  true,
							Sleeping:   true,
							Percentage: lastPct,
							StatusText: "En veille",
							Visual:     icon.VisualSleeping,
							UpdatedAt:  lastPacketAt,
						})
					}
					continue
				}
				// Read error (dongle unplugged or USB reset) -> reconnect.
				reader.Close()
				break
			}

			reading, ok := hid.ParseReport(pkt)
			if !ok {
				continue
			}

			lastPct = reading.Percentage
			lastState = reading.State
			lastPacketAt = time.Now()

			app.UpdateState(tray.State{
				Connected:  true,
				Sleeping:   reading.State == hid.StateSleeping,
				Percentage: reading.Percentage,
				StatusText: reading.State.String(),
				Visual:     mapVisualState(reading.State),
				UpdatedAt:  lastPacketAt,
			})

			// Low-battery alert once per discharge cycle.
			if reading.State == hid.StateDischarging && reading.Percentage <= lowBatteryThreshold && !lowNotified {
				lowNotified = true
				app.NotifyLowBattery(
					"Batterie faible — Attack Shark X11",
					fmt.Sprintf("Il reste %d%% de batterie sur ta souris.", reading.Percentage),
				)
			} else if reading.Percentage >= lowBatteryReset || reading.State == hid.StateCharging || reading.State == hid.StateFull {
				lowNotified = false
			}
			_ = lastState
		}

	reconnect:
		if !waitOrCancel(ctx, 1*time.Second, reconnectCh) {
			return
		}
	}
}

func mapVisualState(s hid.ChargeState) icon.VisualState {
	switch s {
	case hid.StateCharging:
		return icon.VisualCharging
	case hid.StateFull:
		return icon.VisualFull
	case hid.StateSleeping:
		return icon.VisualSleeping
	default:
		return icon.VisualDischarging
	}
}

func waitOrCancel(ctx context.Context, d time.Duration, wake <-chan struct{}) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-wake:
		return true
	case <-t.C:
		return true
	}
}
