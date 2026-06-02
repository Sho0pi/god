package command_test

import (
	"context"
	"strings"
	"testing"

	"github.com/sho0pi/god/command"
)

func TestRegistry_Lookup(t *testing.T) {
	reg := command.NewRegistry(command.Builtin())

	// reset registered via Builtin
	def, ok := reg.Lookup("reset")
	if !ok {
		t.Fatal("expected 'reset' to be registered")
	}
	if def.Name != "reset" {
		t.Errorf("name = %q, want 'reset'", def.Name)
	}

	// help auto-registered
	_, ok = reg.Lookup("help")
	if !ok {
		t.Fatal("expected 'help' to be auto-registered")
	}

	// case-insensitive
	_, ok = reg.Lookup("RESET")
	if !ok {
		t.Error("lookup should be case-insensitive")
	}

	// unknown
	_, ok = reg.Lookup("doesnotexist")
	if ok {
		t.Error("unknown command should not be found")
	}
}

func TestRegistry_Help(t *testing.T) {
	reg := command.NewRegistry(command.Builtin())
	def, _ := reg.Lookup("help")

	var replied string
	req := command.Request{
		Reply: func(text string) error { replied = text; return nil },
	}

	if err := def.Handler(context.Background(), req, nil); err != nil {
		t.Fatalf("help handler error: %v", err)
	}

	if !strings.Contains(replied, "/reset") {
		t.Errorf("help output missing /reset, got: %q", replied)
	}
	if !strings.Contains(replied, "/help") {
		t.Errorf("help output missing /help, got: %q", replied)
	}
}

func TestResetCommand_ClearsHistory(t *testing.T) {
	reg := command.NewRegistry(command.Builtin())
	def, _ := reg.Lookup("reset")

	cleared := false
	var replied string
	req := command.Request{
		Reply: func(text string) error { replied = text; return nil },
	}
	rt := &command.Runtime{
		ClearHistory: func() error { cleared = true; return nil },
	}

	if err := def.Handler(context.Background(), req, rt); err != nil {
		t.Fatalf("reset handler error: %v", err)
	}
	if !cleared {
		t.Error("expected ClearHistory to be called")
	}
	if replied == "" {
		t.Error("expected reply after reset")
	}
}

func TestResetCommand_NoRuntime(t *testing.T) {
	reg := command.NewRegistry(command.Builtin())
	def, _ := reg.Lookup("reset")

	var replied string
	req := command.Request{
		Reply: func(text string) error { replied = text; return nil },
	}

	if err := def.Handler(context.Background(), req, nil); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if replied == "" {
		t.Error("expected fallback reply when runtime is nil")
	}
}

func TestRegistry_All(t *testing.T) {
	reg := command.NewRegistry(command.Builtin())
	all := reg.All()
	// Builtin() has reset + auto-registered help = 2
	if len(all) < 2 {
		t.Errorf("expected at least 2 commands, got %d", len(all))
	}
}
