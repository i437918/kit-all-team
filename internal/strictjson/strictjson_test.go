package strictjson

import "testing"

func TestRejectDuplicateObjectKeys_RejectsDuplicatesAtEveryObjectDepth(t *testing.T) {
	for _, document := range []string{
		`{"value":1,"value":2}`,
		`{"nested":{"value":1,"value":2}}`,
		`{"items":[{"value":1,"value":2}]}`,
	} {
		if err := RejectDuplicateObjectKeys([]byte(document)); err == nil {
			t.Fatalf("accepted duplicate object key: %s", document)
		}
	}
}

func TestRejectDuplicateObjectKeys_AllowsSameKeyInSeparateArrayObjects(t *testing.T) {
	document := []byte(`{"items":[{"value":1},{"value":2}]}`)
	if err := RejectDuplicateObjectKeys(document); err != nil {
		t.Fatalf("rejected keys in separate objects: %v", err)
	}
}
