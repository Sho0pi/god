package memory

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/sho0pi/god/internal/tools"
)

type fakeEmbedder struct {
	vec []float32
	err error
}

func (f *fakeEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return f.vec, f.err
}

type fakeMemStore struct {
	gotConnector, gotUser, gotFact string
	gotVec                         []float32
	err                            error
}

func (f *fakeMemStore) SaveMemory(_ context.Context, connector, userID, fact string, emb []float32) error {
	f.gotConnector, f.gotUser, f.gotFact, f.gotVec = connector, userID, fact, emb
	return f.err
}

func (f *fakeMemStore) SearchMemories(context.Context, string, string, []float32, int) ([]string, error) {
	return nil, nil
}
func (f *fakeMemStore) DeleteMemories(context.Context, string, string) error { return nil }

func run(t *testing.T, e *fakeEmbedder, s *fakeMemStore, ctx context.Context, args string) (tools.Result, error) {
	t.Helper()
	tool := NewRememberTool(e, s)
	return tool.Execute(ctx, json.RawMessage(args))
}

func withUser(connector, user string) context.Context {
	return context.WithValue(context.Background(), tools.UserKey{}, tools.UserInfo{Connector: connector, UserID: user})
}

func TestRemember_SavesFact(t *testing.T) {
	e := &fakeEmbedder{vec: []float32{0.1, 0.2}}
	s := &fakeMemStore{}
	res, err := run(t, e, s, withUser("whatsapp", "u1"), `{"fact":"likes Go"}`)
	if err != nil {
		t.Fatal(err)
	}
	if s.gotFact != "likes Go" || s.gotUser != "u1" || s.gotConnector != "whatsapp" {
		t.Fatalf("store got connector=%q user=%q fact=%q", s.gotConnector, s.gotUser, s.gotFact)
	}
	if len(s.gotVec) != 2 {
		t.Errorf("embedding not passed through: %v", s.gotVec)
	}
	if res.Content != "Remembered: likes Go" {
		t.Errorf("content = %q", res.Content)
	}
}

func TestRemember_NoUserContext(t *testing.T) {
	_, err := run(t, &fakeEmbedder{}, &fakeMemStore{}, context.Background(), `{"fact":"x"}`)
	if err == nil {
		t.Fatal("missing user context must error")
	}
}

func TestRemember_EmptyFact(t *testing.T) {
	_, err := run(t, &fakeEmbedder{}, &fakeMemStore{}, withUser("cli", "u1"), `{"fact":""}`)
	if err == nil {
		t.Fatal("empty fact must error")
	}
}

func TestRemember_EmbedError(t *testing.T) {
	e := &fakeEmbedder{err: errors.New("embed down")}
	_, err := run(t, e, &fakeMemStore{}, withUser("cli", "u1"), `{"fact":"x"}`)
	if err == nil {
		t.Fatal("embed error must propagate")
	}
}

func TestRemember_StoreError(t *testing.T) {
	s := &fakeMemStore{err: errors.New("db down")}
	_, err := run(t, &fakeEmbedder{vec: []float32{1}}, s, withUser("cli", "u1"), `{"fact":"x"}`)
	if err == nil {
		t.Fatal("store error must propagate")
	}
}
