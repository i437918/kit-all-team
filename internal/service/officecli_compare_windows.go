//go:build windows

package service

import "strings"

func officeCLIPathComponentEqual(left, right string) bool { return strings.EqualFold(left, right) }
