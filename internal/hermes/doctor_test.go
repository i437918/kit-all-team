package hermes

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParseDoctorTerminal_AcceptsExactTerminalForms(t *testing.T) {
	valid := map[string][]byte{
		"success":         []byte("earlier section\n\x1b[32m  All checks passed! 🎉\x1b[0m\n"),
		"diagnostics":     []byte("section output\n  Found 2 issue(s) to address:\n\n  1. first\n  2. second\n"),
		"diagnostics-tip": []byte("section output\r\n  Found 1 issue(s) to address:\r\n\r\n  1. any warning meaning is allowed\r\n\r\n  Tip: run 'hermes doctor --fix' to auto-fix what's possible.\r\n"),
	}
	for name, output := range valid {
		t.Run(name, func(t *testing.T) {
			if err := ParseDoctorTerminal(output); err != nil {
				t.Fatalf("ParseDoctorTerminal() error = %v", err)
			}
		})
	}
}

func TestParseDoctorTerminal_AcceptsRichProgressRefresh(t *testing.T) {
	output := []byte("  Running 31 connectivity checks in parallel…\r                                                                      \r  ⚠ OpenRouter API (not configured)\r\n" +
		"  Found 1 issue(s) to address:\r\n\r\n" +
		"  1. OpenRouter is not configured\r\n\r\n" +
		"  Tip: run 'hermes doctor --fix' to auto-fix what's possible.\r\n")
	if err := ParseDoctorTerminal(output); err != nil {
		t.Fatalf("ParseDoctorTerminal() error = %v", err)
	}
}

func TestParseDoctorTerminal_AcceptsCapturedFiveIssueDiagnostics(t *testing.T) {
	output := []byte("  Running 31 connectivity checks in parallel…\r                                                                      \r  ⚠ Optional provider checks completed\r\n" +
		"  Found 5 issue(s) to address:\r\n\r\n" +
		"  1. OpenRouter is not configured\r\n" +
		"  2. Anthropic API key is not configured\r\n" +
		"  3. OpenAI API key is not configured\r\n" +
		"  4. Groq API key is not configured\r\n" +
		"  5. Gemini API key is not configured\r\n\r\n" +
		"  Tip: run 'hermes doctor --fix' to auto-fix what's possible.\r\n")
	if err := ParseDoctorTerminal(output); err != nil {
		t.Fatalf("ParseDoctorTerminal() error = %v", err)
	}
}

func TestParseDoctorTerminal_RejectsIncompleteOrAmbiguousTerminalBlock(t *testing.T) {
	invalid := map[string][]byte{
		"zero":               []byte("  Found 0 issue(s) to address:\n"),
		"negative":           []byte("  Found -1 issue(s) to address:\n  1. first\n"),
		"overflow":           []byte("  Found 999999999999999999999999 issue(s) to address:\n  1. first\n"),
		"skipped":            []byte("  Found 2 issue(s) to address:\n  1. first\n  3. third\n"),
		"duplicate":          []byte("  Found 2 issue(s) to address:\n  1. first\n  1. again\n"),
		"blank-record":       []byte("  Found 1 issue(s) to address:\n  1.   \n"),
		"unknown-in-block":   []byte("  Found 1 issue(s) to address:\nunexpected\n  1. first\n"),
		"trailing":           []byte("  Found 1 issue(s) to address:\n  1. first\ntrailing data\n"),
		"truncated":          []byte("  Found 2 issue(s) to address:\n  1. first\n"),
		"invalid-utf8":       append([]byte("  All checks passed! "), 0xff),
		"standalone-cr":      []byte("unexpected\rtext\n  All checks passed! 🎉\n"),
		"two-terminal-forms": []byte("  Found 1 issue(s) to address:\n  1. first\n  All checks passed! 🎉\n"),
	}
	for name, output := range invalid {
		t.Run(name, func(t *testing.T) {
			if err := ParseDoctorTerminal(output); !errors.Is(err, ErrProfileDoctor) {
				t.Fatalf("ParseDoctorTerminal() error = %v, want ErrProfileDoctor", err)
			}
		})
	}
}

func TestProfileCLI_DoctorBoundsOutputAndWrapsSubprocessFailure(t *testing.T) {
	for name, runner := range map[string]*recordingDoctorRunner{
		"oversize":   {output: []byte(strings.Repeat("x", maxHermesCommandOutput+1) + "\n  All checks passed! 🎉\n")},
		"subprocess": {err: errors.New("sensitive upstream failure")},
	} {
		t.Run(name, func(t *testing.T) {
			client := ProfileCLI{Executable: "hermes", Runner: runner}
			err := client.Doctor(context.Background(), "1c-apa-developer-cc_1c_skills")
			if !errors.Is(err, ErrProfileDoctor) {
				t.Fatalf("Doctor() error = %v, want ErrProfileDoctor", err)
			}
			if strings.Contains(err.Error(), "sensitive upstream failure") {
				t.Fatalf("Doctor() leaked subprocess error: %v", err)
			}
		})
	}
}

type recordingDoctorRunner struct {
	output []byte
	err    error
}

func (r *recordingDoctorRunner) Run(context.Context, string, []string) error { return nil }
func (r *recordingDoctorRunner) Capture(context.Context, string, []string) ([]byte, error) {
	return append([]byte(nil), r.output...), r.err
}
