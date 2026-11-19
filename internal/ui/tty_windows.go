//go:build windows

package ui

import (
	"io"
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode = kernel32.NewProc("SetConsoleMode")
	procSetOutputCP    = kernel32.NewProc("SetConsoleOutputCP")
)

const (
	enableVirtualTerminalProcessing = 0x0004
	enableEchoInput                 = 0x0004
)

func getMode(h syscall.Handle) (uint32, bool) {
	var m uint32
	r, _, _ := procGetConsoleMode.Call(uintptr(h), uintptr(unsafe.Pointer(&m)))
	return m, r != 0
}

func setMode(h syscall.Handle, m uint32) { procSetConsoleMode.Call(uintptr(h), uintptr(m)) }

// enableVT turns on ANSI colors and UTF-8 output in classic conhost.
func enableVT() {
	procSetOutputCP.Call(65001)
	for _, f := range []*os.File{os.Stdout, os.Stderr} {
		h := syscall.Handle(f.Fd())
		if m, ok := getMode(h); ok {
			setMode(h, m|enableVirtualTerminalProcessing)
		}
	}
}

func tty() (io.Reader, func()) {
	f, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return os.Stdin, func() {}
	}
	return f, func() { f.Close() }
}

func noEcho(in io.Reader) func() {
	f, ok := in.(*os.File)
	if !ok {
		return func() {}
	}
	h := syscall.Handle(f.Fd())
	m, ok := getMode(h)
	if !ok {
		return func() {}
	}
	setMode(h, m&^enableEchoInput)
	return func() { setMode(h, m) }
}
