//go:build !windows

package ui

import (
	"io"
	"os"
	"os/exec"
)

func enableVT() {}

func tty() (io.Reader, func()) {
	f, err := os.Open("/dev/tty")
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
	off := exec.Command("stty", "-echo")
	off.Stdin = f
	if off.Run() != nil {
		return func() {}
	}
	return func() {
		on := exec.Command("stty", "echo")
		on.Stdin = f
		_ = on.Run()
	}
}
