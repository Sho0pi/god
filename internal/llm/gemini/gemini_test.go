package gemini

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidate(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") == "" {
			t.Error("missing key query param")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ok.Close()

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer bad.Close()

	orig := validateBaseURL
	defer func() { validateBaseURL = orig }()

	validateBaseURL = ok.URL
	if err := Validate(context.Background(), "gem-key"); err != nil {
		t.Errorf("valid key should pass: %v", err)
	}

	validateBaseURL = bad.URL
	if err := Validate(context.Background(), "gem-key"); err == nil {
		t.Error("400 should fail validation")
	}

	if err := Validate(context.Background(), ""); err == nil {
		t.Error("empty key should fail")
	}
}
