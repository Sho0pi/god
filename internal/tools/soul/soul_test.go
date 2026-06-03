package soul

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sho0pi/god/internal/tools"
)

type fakeSoulStore struct {
	gotConnector, gotUser, gotSoul string
	err                            error
}

func (f *fakeSoulStore) AssignSoul(_ context.Context, connector, userID, soul string) error {
	f.gotConnector, f.gotUser, f.gotSoul = connector, userID, soul
	return f.err
}
func (f *fakeSoulStore) GetSoul(context.Context, string, string) (string, error) { return "", nil }
func (f *fakeSoulStore) DeleteSoul(context.Context, string, string) error        { return nil }

func exec(t *testing.T, s *fakeSoulStore, ctx context.Context, args string) (tools.Result, error) {
	t.Helper()
	return NewSetSoulTool(s, []string{"human", "caveman"}).Execute(ctx, json.RawMessage(args))
}

func withUser(connector, user string) context.Context {
	return context.WithValue(context.Background(), tools.UserKey{}, tools.UserInfo{Connector: connector, UserID: user})
}

func TestSetSoul_Assigns(t *testing.T) {
	s := &fakeSoulStore{}
	res, err := exec(t, s, withUser("whatsapp", "u1"), `{"soul":"caveman","reason":"terse dev"}`)
	if err != nil {
		t.Fatal(err)
	}
	if s.gotSoul != "caveman" || s.gotUser != "u1" || s.gotConnector != "whatsapp" {
		t.Fatalf("store got connector=%q user=%q soul=%q", s.gotConnector, s.gotUser, s.gotSoul)
	}
	if res.Data["soul"] != "caveman" {
		t.Errorf("data soul = %v", res.Data["soul"])
	}
}

func TestSetSoul_EnumInSchema(t *testing.T) {
	tool := NewSetSoulTool(&fakeSoulStore{}, []string{"human", "caveman"})
	enum := tool.Schema().Properties["soul"].Enum
	if len(enum) != 2 || enum[0] != "human" {
		t.Fatalf("enum = %v, want [human caveman]", enum)
	}
}

func TestSetSoul_NoUserContext(t *testing.T) {
	if _, err := exec(t, &fakeSoulStore{}, context.Background(), `{"soul":"human"}`); err == nil {
		t.Fatal("missing user context must error")
	}
}

func TestSetSoul_EmptySoul(t *testing.T) {
	if _, err := exec(t, &fakeSoulStore{}, withUser("cli", "u1"), `{"soul":""}`); err == nil {
		t.Fatal("empty soul must error")
	}
}

func TestSetSoul_StoreError(t *testing.T) {
	s := &fakeSoulStore{err: errors.New("db down")}
	if _, err := exec(t, s, withUser("cli", "u1"), `{"soul":"human"}`); err == nil {
		t.Fatal("store error must propagate")
	}
}
