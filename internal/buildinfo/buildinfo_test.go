package buildinfo

import "testing"

func TestCurrentIsStableAndNonEmpty(t *testing.T) {
	first := Current()
	second := Current()

	if first != second {
		t.Fatalf("Current() is not stable: first=%+v second=%+v", first, second)
	}
	if first.Version == "" || first.Commit == "" || first.BuildDate == "" {
		t.Fatalf("build metadata contains an empty field: %+v", first)
	}
}
