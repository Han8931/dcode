package tutor

import (
	"testing"

	"dcode/internal/config"
)

// The tutor must go offline ONLY when the endpoint actually requires a key
// (the official OpenAI URL) and none is set. Local servers and gateways
// without keys must stay online.
func TestOfflineHeuristic(t *testing.T) {
	t.Setenv("MEARI_TEST_KEY", "")

	cases := []struct {
		name    string
		cfg     config.AIConfig
		offline bool
	}{
		{"openai default, no key", config.AIConfig{Provider: "openai", APIKeyEnv: "MEARI_TEST_KEY"}, true},
		{"ollama, no key", config.AIConfig{Provider: "ollama"}, false},
		{"compatible local, no key", config.AIConfig{Provider: "compatible", BaseURL: "http://localhost:1234/v1"}, false},
		{"openai behind custom proxy, no key", config.AIConfig{Provider: "openai", BaseURL: "http://localhost:9999/v1"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := New(c.cfg).Offline(); got != c.offline {
				t.Fatalf("Offline() = %v, want %v", got, c.offline)
			}
		})
	}

	t.Run("openai default with key", func(t *testing.T) {
		t.Setenv("MEARI_TEST_KEY2", "sk-test")
		cfg := config.AIConfig{Provider: "openai", APIKeyEnv: "MEARI_TEST_KEY2"}
		if New(cfg).Offline() {
			t.Fatal("with a key set, the tutor must be online")
		}
	})

	t.Run("openai with literal api_key in config", func(t *testing.T) {
		cfg := config.AIConfig{Provider: "openai", APIKeyEnv: "MEARI_TEST_KEY", APIKey: "sk-direct"}
		tut := New(cfg)
		if tut.Offline() {
			t.Fatal("a literal api_key must count as a key")
		}
		if tut.apiKey != "sk-direct" {
			t.Fatalf("apiKey = %q, want the config literal", tut.apiKey)
		}
	})

	t.Run("env var wins over literal", func(t *testing.T) {
		t.Setenv("MEARI_TEST_KEY3", "sk-from-env")
		cfg := config.AIConfig{Provider: "openai", APIKeyEnv: "MEARI_TEST_KEY3", APIKey: "sk-direct"}
		if got := New(cfg).apiKey; got != "sk-from-env" {
			t.Fatalf("apiKey = %q, want the env value", got)
		}
	})
}

func TestTimeoutConfigurable(t *testing.T) {
	tut := New(config.AIConfig{Provider: "ollama", TimeoutSeconds: 300})
	if got := tut.client.Timeout.Seconds(); got != 300 {
		t.Fatalf("timeout = %vs, want 300s", got)
	}
	tut = New(config.AIConfig{Provider: "ollama"})
	if got := tut.client.Timeout; got != defaultTimeout {
		t.Fatalf("default timeout = %v, want %v", got, defaultTimeout)
	}
}
