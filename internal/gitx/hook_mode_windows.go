package gitx

import "io/fs"

func hookModeReady(fs.FileInfo) bool { return true }

func repairManagedHookMode(string, string) error { return nil }
