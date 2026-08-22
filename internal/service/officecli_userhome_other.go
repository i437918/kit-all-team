//go:build !windows

package service

import "os"

func officeCLIUserHome() (string, error) { return os.UserHomeDir() }
