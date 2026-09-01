package service

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMain(m *testing.M) {
	bin, err := os.MkdirTemp("", "teamkit-service-apps-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, name := range []string{"cursor", "codex"} {
		path := filepath.Join(bin, name)
		contents := []byte("#!/bin/sh\nexit 0\n")
		if runtime.GOOS == "windows" {
			path += ".cmd"
			contents = []byte("@exit /b 0\r\n")
		}
		if err := os.WriteFile(path, contents, 0o700); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if err := os.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	if err := os.RemoveAll(bin); err != nil && code == 0 {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}
