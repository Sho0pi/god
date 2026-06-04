package command_test

import (
	"context"
	"strings"
	"testing"

	"github.com/sho0pi/god/internal/command"
)

// fakeRuntime is a configurable command.Runtime for testing handlers in isolation.
type fakeRuntime struct {
	onClear func() error
	admin   bool
}

func (f *fakeRuntime) ClearHistory() error {
	if f.onClear != nil {
		return f.onClear()
	}
	return nil
}
func (f *fakeRuntime) IsAdmin() bool                           { return f.admin }
func (f *fakeRuntime) FactoryReset() error                     { return nil }
func (f *fakeRuntime) Info() command.UserInfo                  { return command.UserInfo{} }
func (f *fakeRuntime) AllowAdd(string) error                   { return nil }
func (f *fakeRuntime) AllowRemove(string) error                { return nil }
func (f *fakeRuntime) AllowList() ([]string, error)            { return nil, nil }
func (f *fakeRuntime) ResolveApproval(approve bool, id string) {}
func (f *fakeRuntime) GenerateLinkCode() (string, error)       { return "ABC123", nil }
func (f *fakeRuntime) RedeemLinkCode(string) (string, error)   { return "whatsapp:1", nil }
func (f *fakeRuntime) Unlink() error                           { return nil }
func (f *fakeRuntime) LinkStatus() (bool, string)              { return false, "" }
func (f *fakeRuntime) ListReminders() ([]string, error)        { return []string{"#1  [1m]  say hi"}, nil }
func (f *fakeRuntime) CancelReminder(int64) (bool, error)      { return true, nil }

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
	// Usages are backticked so they render as tap-to-copy code in chat.
	if !strings.Contains(replied, "`/help`") {
		t.Errorf("help output should backtick commands, got: %q", replied)
	}
}

func TestLinkCommand_NoArgGeneratesCode(t *testing.T) {
	reg := command.NewRegistry(command.Builtin())
	def, ok := reg.Lookup("link")
	if !ok {
		t.Fatal("expected 'link' to be registered")
	}
	var replied string
	req := command.Request{Text: "/link", Reply: func(t string) error { replied = t; return nil }}
	if err := def.Handler(context.Background(), req, &fakeRuntime{}); err != nil {
		t.Fatalf("link handler error: %v", err)
	}
	if !strings.Contains(replied, "ABC123") {
		t.Errorf("expected code in reply, got %q", replied)
	}
}

func TestLinkCommand_WithCodeRedeems(t *testing.T) {
	reg := command.NewRegistry(command.Builtin())
	def, _ := reg.Lookup("link")
	var replied string
	req := command.Request{Text: "/link XYZ", Reply: func(t string) error { replied = t; return nil }}
	if err := def.Handler(context.Background(), req, &fakeRuntime{}); err != nil {
		t.Fatalf("link redeem error: %v", err)
	}
	if !strings.Contains(strings.ToLower(replied), "linked") || !strings.Contains(replied, "whatsapp:1") {
		t.Errorf("expected linked confirmation, got %q", replied)
	}
}

func TestUnlinkCommand(t *testing.T) {
	reg := command.NewRegistry(command.Builtin())
	def, ok := reg.Lookup("unlink")
	if !ok {
		t.Fatal("expected 'unlink' to be registered")
	}
	var replied string
	req := command.Request{Text: "/unlink", Reply: func(t string) error { replied = t; return nil }}
	if err := def.Handler(context.Background(), req, &fakeRuntime{}); err != nil {
		t.Fatalf("unlink handler error: %v", err)
	}
	if !strings.Contains(strings.ToLower(replied), "unlinked") {
		t.Errorf("expected unlink confirmation, got %q", replied)
	}
}

func TestRemindersCommand_List(t *testing.T) {
	reg := command.NewRegistry(command.Builtin())
	def, ok := reg.Lookup("reminders")
	if !ok {
		t.Fatal("expected 'reminders' to be registered")
	}
	var replied string
	req := command.Request{Text: "/reminders", Reply: func(t string) error { replied = t; return nil }}
	if err := def.Handler(context.Background(), req, &fakeRuntime{}); err != nil {
		t.Fatalf("reminders handler: %v", err)
	}
	if !strings.Contains(replied, "#1") || !strings.Contains(replied, "say hi") {
		t.Errorf("expected reminder list, got %q", replied)
	}
}

func TestRemindersCommand_Cancel(t *testing.T) {
	reg := command.NewRegistry(command.Builtin())
	def, _ := reg.Lookup("reminders")
	var replied string
	req := command.Request{Text: "/reminders cancel 1", Reply: func(t string) error { replied = t; return nil }}
	if err := def.Handler(context.Background(), req, &fakeRuntime{}); err != nil {
		t.Fatalf("cancel handler: %v", err)
	}
	if !strings.Contains(strings.ToLower(replied), "cancelled") {
		t.Errorf("expected cancel confirmation, got %q", replied)
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
	rt := &fakeRuntime{onClear: func() error { cleared = true; return nil }}

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
