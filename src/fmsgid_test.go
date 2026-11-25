package main

import (
	"net/http"
	"testing"
)

func TestStringPtrEqual(t *testing.T) {
	a, b, c := "x", "x", "y"
	if !stringPtrEqual(nil, nil) {
		t.Error("nil == nil should be true")
	}
	if stringPtrEqual(&a, nil) || stringPtrEqual(nil, &a) {
		t.Error("non-nil == nil should be false")
	}
	if !stringPtrEqual(&a, &b) {
		t.Error("equal values should be true")
	}
	if stringPtrEqual(&a, &c) {
		t.Error("different values should be false")
	}
}

func TestPostHandlers_InvalidJSON(t *testing.T) {
	for _, route := range []string{"/fmsgid", "/fmsgid/send", "/fmsgid/recv", "/fmsgid/send/release", "/fmsgid/recv/release"} {
		if w := serveRoute("POST", route, "{not-json"); w.Code != http.StatusBadRequest {
			t.Errorf("POST %s invalid json: status %d, want 400", route, w.Code)
		}
	}
}
