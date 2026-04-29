package apikeyscope

import (
	"testing"

	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestNormalizeSupplierKey(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "openai compat", input: "openai-compatibility:MyGW", want: "openai-compatibility:mygw"},
		{name: "claude base", input: "claude-api-key:https://API.Anthropic.com/", want: "claude-api-key:https://api.anthropic.com"},
		{name: "codex default", input: "codex-api-key:", want: "codex-api-key:default"},
		{name: "oauth provider", input: "oauth:Gemini-CLI", want: "oauth:gemini-cli"},
		{name: "invalid kind", input: "gemini:abc", wantErr: true},
		{name: "missing separator", input: "openai-compatibility", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeSupplierKey(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NormalizeSupplierKey(%q) expected error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeSupplierKey(%q) err = %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeSupplierKey(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSupplierKeyFromAuth(t *testing.T) {
	tests := []struct {
		name string
		auth *coreauth.Auth
		want string
	}{
		{
			name: "claude config key",
			auth: &coreauth.Auth{
				Provider: "claude",
				Attributes: map[string]string{
					"source":   "config:claude[token]",
					"base_url": "https://api.anthropic.com/",
				},
			},
			want: "claude-api-key:https://api.anthropic.com",
		},
		{
			name: "codex config key",
			auth: &coreauth.Auth{
				Provider: "codex",
				Attributes: map[string]string{
					"source":   "config:codex[token]",
					"base_url": "https://api.openai.com",
				},
			},
			want: "codex-api-key:https://api.openai.com",
		},
		{
			name: "openai compat",
			auth: &coreauth.Auth{
				Provider: "mygw",
				Attributes: map[string]string{
					"source":      "config:mygw[token]",
					"compat_name": "MyGW",
				},
			},
			want: "openai-compatibility:mygw",
		},
		{
			name: "oauth auth",
			auth: &coreauth.Auth{
				Provider: "gemini-cli",
				Attributes: map[string]string{
					"auth_kind": "oauth",
					"source":    "/tmp/auths/gemini.json",
				},
			},
			want: "oauth:gemini-cli",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SupplierKeyFromAuth(tt.auth); got != tt.want {
				t.Fatalf("SupplierKeyFromAuth() = %q, want %q", got, tt.want)
			}
		})
	}
}
