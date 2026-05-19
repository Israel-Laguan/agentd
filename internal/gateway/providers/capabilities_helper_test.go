package providers

import "testing"

func TestSupportsChatTools(t *testing.T) {
	t.Parallel()

	tests := []struct {
		provider string
		want     bool
	}{
		{"openai", true},
		{"OPENAI", true},
		{"anthropic", true},
		{"Anthropic", true},
		{"ollama", false},
		{"llamacpp", false},
		{"horde", false},
		{"", false},
		{"unknown", false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.provider, func(t *testing.T) {
			t.Parallel()
			if got := SupportsChatTools(tt.provider); got != tt.want {
				t.Errorf("SupportsChatTools(%q) = %v, want %v", tt.provider, got, tt.want)
			}
		})
	}
}
