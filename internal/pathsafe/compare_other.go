//go:build !windows

package pathsafe

func canonicalPath(path string) (string, error) { return path, nil }

func comparisonKey(path string) string { return path }
