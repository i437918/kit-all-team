//go:build !windows

package service

func officeCLIPathComponentEqual(left, right string) bool { return left == right }
