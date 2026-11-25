//go:build darwin

package main

import (
	"fmt"
	"path/filepath"
	"sort"
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
	if err := syscall.SetNonblock(fd, false); err != nil {
		_ = syscall.Close(fd)
		return -1, err
	}
	return fd, nil
}

func configureTermios(fd int, baud int) error {
	var rate uint64
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
		return fmt.Errorf("unsupported baud rate on macOS: %d", baud)
	}

	var t syscall.Termios
	if _, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd), syscall.TIOCGETA, uintptr(unsafe.Pointer(&t)), 0, 0, 0); errno != 0 {
		return errno
	}
	t.Iflag = 0
	t.Oflag = 0
	t.Cflag = syscall.CS8 | syscall.CREAD | syscall.CLOCAL
	t.Lflag = 0
	t.Ispeed = rate
	t.Ospeed = rate
	t.Cc[syscall.VMIN] = 0
	t.Cc[syscall.VTIME] = 2 // 200ms read timeout

	if _, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd), syscall.TIOCSETA, uintptr(unsafe.Pointer(&t)), 0, 0, 0); errno != 0 {
		return errno
	}
	return nil
}

func detectSerialPorts() []string {
	var found []string
	for _, pat := range []string{"/dev/cu.usbmodem*", "/dev/cu.usbserial*"} {
		matches, _ := filepath.Glob(pat)
		sort.Strings(matches)
		found = append(found, matches...)
	}
	return found
}
