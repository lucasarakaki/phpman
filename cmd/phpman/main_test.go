package main

import (
	"reflect"
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
