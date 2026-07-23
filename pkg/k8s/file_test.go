package k8s

import "testing"

func TestShellSingleQuote(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/tmp/file.txt", "'/tmp/file.txt'"},
		{"/tmp/file with spaces.txt", "'/tmp/file with spaces.txt'"},
		{"/tmp/file'with'quotes.txt", "'/tmp/file'\"'\"'with'\"'\"'quotes.txt'"},
		{"", "''"},
		{"simple", "'simple'"},
	}

	for _, test := range tests {
		result := shellSingleQuote(test.input)
		if result != test.expected {
			t.Errorf("shellSingleQuote(%q) = %q, expected %q", test.input, result, test.expected)
		}
	}
}
