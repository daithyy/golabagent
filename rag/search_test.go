package rag

import "testing"

func TestOrTSQuery(t *testing.T) {
	got := orTSQuery("Why is region R11 claiming more? Region frequency!")
	want := "why | is | region | r11 | claiming | more | frequency"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if orTSQuery("?!") != "" {
		t.Error("expected empty query for punctuation only")
	}
}
