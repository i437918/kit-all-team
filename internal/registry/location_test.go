package registry

import "testing"

func TestDefaultPath_PlatformMatrix(t *testing.T) {
	tests := []struct {
		goos       string
		env        map[string]string
		home, want string
	}{
		{"windows", map[string]string{"LOCALAPPDATA": `C:\Users\D\AppData\Local`}, `C:\Users\D`, `C:\Users\D\AppData\Local\TeamKit\environments.json`},
		{"darwin", nil, "/Users/d", "/Users/d/Library/Application Support/TeamKit/environments.json"},
		{"linux", map[string]string{"XDG_CONFIG_HOME": "/cfg"}, "/home/d", "/cfg/teamkit/environments.json"},
		{"linux", nil, "/home/d", "/home/d/.config/teamkit/environments.json"},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			got, err := DefaultPath(LocationOptions{
				GOOS:        tt.goos,
				Getenv:      func(key string) string { return tt.env[key] },
				UserHomeDir: func() (string, error) { return tt.home, nil },
			})
			if err != nil || got != tt.want {
				t.Fatalf("got=%q want=%q err=%v", got, tt.want, err)
			}
		})
	}
}

func TestDefaultPath_RejectsUnavailableOrRelativeBases(t *testing.T) {
	tests := []LocationOptions{
		{GOOS: "windows", Getenv: func(string) string { return "relative" }, UserHomeDir: func() (string, error) { return "", nil }},
		{GOOS: "darwin", Getenv: func(string) string { return "" }, UserHomeDir: func() (string, error) { return "relative", nil }},
		{GOOS: "linux", Getenv: func(string) string { return "relative" }, UserHomeDir: func() (string, error) { return "/home/d", nil }},
		{GOOS: "plan9", Getenv: func(string) string { return "" }, UserHomeDir: func() (string, error) { return "/home/d", nil }},
	}
	for _, options := range tests {
		if got, err := DefaultPath(options); err == nil || got != "" {
			t.Fatalf("goos=%s got=%q err=%v", options.GOOS, got, err)
		}
	}
}
