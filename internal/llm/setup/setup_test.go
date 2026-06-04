package setup

import "testing"

func TestProvidersRegistered(t *testing.T) {
	want := map[string]string{
		"gemini":    "GEMINI_API_KEY",
		"openai":    "OPENAI_API_KEY",
		"anthropic": "ANTHROPIC_API_KEY",
	}
	for key, envVar := range want {
		p, ok := Lookup(key)
		if !ok {
			t.Errorf("%q provider not registered", key)
			continue
		}
		if p.EnvVar() != envVar {
			t.Errorf("%q EnvVar = %q, want %q", key, p.EnvVar(), envVar)
		}
		if len(p.Models()) == 0 {
			t.Errorf("%q should list at least one model", key)
		}
		if p.Title() == "" {
			t.Errorf("%q should have a title", key)
		}
	}
	if len(All()) < 3 {
		t.Errorf("expected >=3 providers, got %d", len(All()))
	}
}
