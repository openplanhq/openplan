package domain

import "testing"

func TestTemplateRunLogStateValid(t *testing.T) {
	for _, state := range []TemplateRunLogState{TemplateRunLogLive, TemplateRunLogComplete, TemplateRunLogLost} {
		if !state.Valid() {
			t.Fatalf("%q should be valid", state)
		}
	}
	if TemplateRunLogState("done").Valid() {
		t.Fatal("done should not be valid")
	}
}
