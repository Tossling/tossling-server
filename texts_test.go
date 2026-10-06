package main

import (
	"net/http/httptest"
	"testing"
)

func TestTextsHaveTheSameKeys(t *testing.T) {
	for key := range english {
		if _, ok := russian[key]; !ok {
			t.Errorf("russian has no %q", key)
		}
	}
	for key := range russian {
		if _, ok := english[key]; !ok {
			t.Errorf("english has no %q", key)
		}
	}
}

func TestSessionSignature(t *testing.T) {
	a := &adminPanel{}
	if a.sign("key", 100) == a.sign("other", 100) || a.sign("key", 100) == a.sign("key", 101) {
		t.Fatal("the session signature must depend on the key and the expiry")
	}
}

func TestFormsKeepTheirOrigin(t *testing.T) {
	w := httptest.NewRecorder()
	render(w, 200, "lost.html", pageData{T: english})
	if policy := w.Header().Get("Referrer-Policy"); policy == "no-referrer" || policy == "" {
		t.Fatalf("Referrer-Policy %q makes browsers send Origin: null on form posts, and sameOrigin refuses them", policy)
	}
}
