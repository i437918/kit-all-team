package securityaudit

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseReachableGitObjectIDs_AcceptsPortableLineContract(t *testing.T) {
	first := strings.Repeat("a", 40)
	second := strings.Repeat("b", 40)

	got, err := parseReachableGitObjectIDs([]byte(first + "\n" + second + "\n"))
	if err != nil {
		t.Fatalf("parseReachableGitObjectIDs: %v", err)
	}
	want := []string{first, second}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("object IDs = %v, want %v", got, want)
	}
}

func TestParseReachableGitObjectIDs_RejectsVersionDependentMetadata(t *testing.T) {
	object := strings.Repeat("c", 40)
	for name, input := range map[string][]byte{
		"legacy object name": []byte(object + " path/to/file\n"),
		"new NUL metadata":   []byte(object + "\x00path=path/to/file\x00"),
		"missing terminator": []byte(object),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseReachableGitObjectIDs(input); err == nil {
				t.Fatal("parseReachableGitObjectIDs accepted output outside the portable command contract")
			}
		})
	}
}
