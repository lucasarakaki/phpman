package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestReorderPlanArgs(t *testing.T) {
	got := reorderPlanArgs([]string{"8.3", "--dry-run", "--json"})
	want := []string{"--dry-run", "--json", "8.3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestReorderPlanArgsFlagsFirst(t *testing.T) {
	got := reorderPlanArgs([]string{"--dry-run", "8.3"})
	want := []string{"--dry-run", "8.3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestConfirmPlan(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"s\n", true},
		{"sim\n", true},
		{"Y\n", true},
		{"yes\n", true},
		{"\n", false},
		{"n\n", false},
		{"talvez\n", false},
	}
	for _, tc := range cases {
		var out bytes.Buffer
		got, err := confirmPlan(&out, strings.NewReader(tc.input))
		if err != nil {
			t.Fatalf("confirmPlan(%q): %v", tc.input, err)
		}
		if got != tc.want {
			t.Errorf("confirmPlan(%q) = %v, esperado %v", tc.input, got, tc.want)
		}
		if !strings.Contains(out.String(), "Executar este plano?") {
			t.Errorf("prompt ausente: %q", out.String())
		}
	}
}
