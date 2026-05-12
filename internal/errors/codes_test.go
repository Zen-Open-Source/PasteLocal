package errors

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegistryCodesExist(t *testing.T) {
	want := map[string]int{
		"CB1001": http.StatusBadRequest,
		"CB1002": http.StatusInternalServerError,
		"CB1003": http.StatusInternalServerError,
		"CB1004": http.StatusUnsupportedMediaType,
		"CB1005": http.StatusRequestEntityTooLarge,
		"CB2001": http.StatusUnauthorized,
		"CB2002": http.StatusUnauthorized,
		"CB3001": http.StatusUpgradeRequired,
		"CB4001": http.StatusTooManyRequests,
	}

	for code, wantStatus := range want {
		t.Run(code, func(t *testing.T) {
			tmpl, ok := Registry[code]
			if !ok {
				t.Fatalf("code %q not found in registry", code)
			}
			if tmpl.HTTPStatus != wantStatus {
				t.Errorf("code %q: got HTTP status %d, want %d", code, tmpl.HTTPStatus, wantStatus)
			}
			if tmpl.Code != code {
				t.Errorf("code %q: got Code field %q, want %q", code, tmpl.Code, code)
			}
			if tmpl.Message == "" {
				t.Errorf("code %q: Message is empty", code)
			}
			if tmpl.FixHint == "" {
				t.Errorf("code %q: FixHint is empty", code)
			}
		})
	}
}

func TestNew(t *testing.T) {
	e := New("CB1001")
	if e.Code != "CB1001" {
		t.Errorf("got Code %q, want %q", e.Code, "CB1001")
	}
	if e.HTTPStatus != http.StatusBadRequest {
		t.Errorf("got HTTPStatus %d, want %d", e.HTTPStatus, http.StatusBadRequest)
	}
	if e.Message != "No image on clipboard" {
		t.Errorf("got Message %q, want %q", e.Message, "No image on clipboard")
	}
	if e.FixHint != "Take a screenshot first" {
		t.Errorf("got FixHint %q, want %q", e.FixHint, "Take a screenshot first")
	}
}

func TestNewWithMessage(t *testing.T) {
	customMsg := "clipboard returned empty data"
	e := NewWithMessage("CB1003", customMsg)
	if e.Code != "CB1003" {
		t.Errorf("got Code %q, want %q", e.Code, "CB1003")
	}
	if e.HTTPStatus != http.StatusInternalServerError {
		t.Errorf("got HTTPStatus %d, want %d", e.HTTPStatus, http.StatusInternalServerError)
	}
	if e.Message != customMsg {
		t.Errorf("got Message %q, want %q", e.Message, customMsg)
	}
	// FixHint should still come from the template.
	if e.FixHint != "Check `pastelocal logs`" {
		t.Errorf("got FixHint %q, want %q", e.FixHint, "Check `pastelocal logs`")
	}
}

func TestNewUnknownCodePanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for unknown code, but did not panic")
		}
		msg := fmt.Sprintf("%s", r)
		if !strings.Contains(msg, "CB9999") {
			t.Errorf("panic message %q does not contain unknown code", msg)
		}
	}()
	New("CB9999")
}

func TestNewWithMessageUnknownCodePanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for unknown code, but did not panic")
		}
	}()
	NewWithMessage("CB9999", "custom")
}

func TestWriteJSON(t *testing.T) {
	e := New("CB2001")
	rec := httptest.NewRecorder()
	WriteJSON(rec, e)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got HTTP status %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("got Content-Type %q, want %q", ct, "application/json")
	}

	var got jsonResp
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	if got.OK {
		t.Error("got ok=true, want false")
	}
	if got.Code != "CB2001" {
		t.Errorf("got Code %q, want %q", got.Code, "CB2001")
	}
	if got.Error != "Invalid auth token" {
		t.Errorf("got Error %q, want %q", got.Error, "Invalid auth token")
	}
	wantHint := "Re-run `pastelocal add-host <host>` to sync the token."
	if got.FixHint != wantHint {
		t.Errorf("got FixHint %q, want %q", got.FixHint, wantHint)
	}
}

func TestWriteJSONCustomMessage(t *testing.T) {
	e := NewWithMessage("CB1004", "unsupported format: BMP")
	rec := httptest.NewRecorder()
	WriteJSON(rec, e)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("got HTTP status %d, want %d", rec.Code, http.StatusUnsupportedMediaType)
	}

	var got jsonResp
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	if got.Error != "unsupported format: BMP" {
		t.Errorf("got Error %q, want %q", got.Error, "unsupported format: BMP")
	}
	if got.Code != "CB1004" {
		t.Errorf("got Code %q, want %q", got.Code, "CB1004")
	}
	if got.FixHint != "Save as PNG manually" {
		t.Errorf("got FixHint %q, want %q", got.FixHint, "Save as PNG manually")
	}
}

func TestErrorImplementsError(t *testing.T) {
	e := New("CB1001")
	_ = error(e) // compile-time check
	msg := e.Error()
	if !strings.Contains(msg, "CB1001") {
		t.Errorf("Error() %q does not contain code", msg)
	}
	if !strings.Contains(msg, "No image on clipboard") {
		t.Errorf("Error() %q does not contain message", msg)
	}
}
