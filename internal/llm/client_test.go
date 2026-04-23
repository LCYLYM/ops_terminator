package llm

import "testing"

func TestChatCompletionsURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{
			name:    "root base URL uses standard path",
			baseURL: "https://api.openai.com",
			want:    "https://api.openai.com/v1/chat/completions",
		},
		{
			name:    "openai compatible prefix stays in base URL",
			baseURL: "https://api.longcat.chat/openai",
			want:    "https://api.longcat.chat/openai/v1/chat/completions",
		},
		{
			name:    "v1 suffix does not duplicate version segment",
			baseURL: "https://api.longcat.chat/openai/v1",
			want:    "https://api.longcat.chat/openai/v1/chat/completions",
		},
		{
			name:    "full endpoint stays unchanged",
			baseURL: "https://api.longcat.chat/openai/v1/chat/completions",
			want:    "https://api.longcat.chat/openai/v1/chat/completions",
		},
		{
			name:    "trailing slash is ignored",
			baseURL: "https://api.example.com/",
			want:    "https://api.example.com/v1/chat/completions",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := chatCompletionsURL(tt.baseURL); got != tt.want {
				t.Fatalf("chatCompletionsURL(%q) = %q, want %q", tt.baseURL, got, tt.want)
			}
		})
	}
}
