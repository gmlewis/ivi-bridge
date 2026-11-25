//go:build linux

package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

func openSerialPort(path string, baud int) (int, error) {
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NOCTTY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return -1, err
	}
	if err := configureTermios(fd, baud); err != nil {
		_ = syscall.Close(fd)
		return -1, err
	}
	// Clear O_NONBLOCK so VTIME controls read timeouts cleanly.
	if err := syscall.SetNonblock(fd, false); err != nil {
		_ = syscall.Close(fd)
		return -1, err
	}
	return fd, nil
}

func configureTermios(fd int, baud int) error {
	var rate uint32
	switch baud {
	case 9600:
		rate = syscall.B9600
	case 19200:
		rate = syscall.B19200
	case 38400:
		rate = syscall.B38400
	case 57600:
		rate = syscall.B57600
	case 115200:
		rate = syscall.B115200
	case 230400:
		rate = syscall.B230400
	default:
		return fmt.Errorf("unsupported baud rate on linux: %d", baud)
	}

	var t syscall.Termios
	if _, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd), syscall.TCGETS, uintptr(unsafe.Pointer(&t)), 0, 0, 0); errno != 0 {
		return errno
	}
	t.Iflag = 0
	t.Oflag = 0
	t.Cflag = syscall.CS8 | syscall.CREAD | syscall.CLOCAL | rate
	t.Lflag = 0
	t.Ispeed = rate
	t.Ospeed = rate
	t.Cc[syscall.VMIN] = 0
	t.Cc[syscall.VTIME] = 2 // 200ms read timeout

	if _, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd), syscall.TCSETS, uintptr(unsafe.Pointer(&t)), 0, 0, 0); errno != 0 {
		return errno
	}
	return nil
}

func detectSerialPorts() []string {
	var found []string
	seen := map[string]bool{}
	add := func(matches []string) {
		sort.Strings(matches)
		for _, m := range matches {
			if !seen[m] {
				seen[m] = true
				found = append(found, m)
			}
		}
	}
	// Prefer STMicroelectronics STM32F407 (IVI printer) by-id symlinks first.
	byID, _ := filepath.Glob("/dev/serial/by-id/*")
	sort.Strings(byID)
	for _, p := range byID {
		low := strings.ToLower(p)
		if strings.Contains(low, "stmicro") || strings.Contains(low, "f407") {
			add([]string{p})
		}
	}
	add(byID)
	acm, _ := filepath.Glob("/dev/ttyACM*")
	add(acm)
	usb, _ := filepath.Glob("/dev/ttyUSB*")
	add(usb)
	return found
}
