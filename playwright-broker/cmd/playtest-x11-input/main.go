// playtest-x11-input emits real X11 pointer input for Chromium sessions running
// on the broker-owned virtual display. DevTools mouse movement is not suitable
// for pointer-lock games because Chromium reports synthetic recenter movement.
package main

/*
#cgo LDFLAGS: -lX11 -lXtst
#include <X11/Xlib.h>
#include <X11/extensions/XTest.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"strconv"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	display := C.XOpenDisplay(nil)
	if display == nil {
		return errors.New("opening X11 display from DISPLAY")
	}
	defer C.XCloseDisplay(display)

	if len(args) == 3 && args[0] == "move-absolute" {
		x, err := integer(args[1], "x")
		if err != nil {
			return err
		}
		y, err := integer(args[2], "y")
		if err != nil {
			return err
		}
		if C.XTestFakeMotionEvent(display, -1, C.int(x), C.int(y), 0) == 0 {
			return errors.New("XTestFakeMotionEvent failed")
		}
	} else if len(args) == 3 && args[0] == "move-relative" {
		dx, err := integer(args[1], "delta x")
		if err != nil {
			return err
		}
		dy, err := integer(args[2], "delta y")
		if err != nil {
			return err
		}
		if C.XTestFakeRelativeMotionEvent(display, C.int(dx), C.int(dy), 0) == 0 {
			return errors.New("XTestFakeRelativeMotionEvent failed")
		}
	} else if len(args) == 3 && args[0] == "button" {
		button, err := integer(args[1], "button")
		if err != nil || button < 1 || button > 9 {
			return fmt.Errorf("button must be an integer from 1 to 9")
		}
		pressed, err := boolean(args[2])
		if err != nil {
			return err
		}
		state := C.Bool(0)
		if pressed {
			state = C.Bool(1)
		}
		if C.XTestFakeButtonEvent(display, C.uint(button), state, 0) == 0 {
			return errors.New("XTestFakeButtonEvent failed")
		}
	} else {
		return errors.New("usage: playtest-x11-input move-absolute X Y | move-relative DX DY | button NUMBER down|up")
	}

	C.XFlush(display)
	return nil
}

func integer(raw string, name string) (int, error) {
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	return value, nil
}

func boolean(raw string) (bool, error) {
	switch raw {
	case "down":
		return true, nil
	case "up":
		return false, nil
	default:
		return false, errors.New("button state must be down or up")
	}
}
