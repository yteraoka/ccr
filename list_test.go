package main

import "testing"

func TestAttachRunningPIDs(t *testing.T) {
	entries := []sessionEntry{
		{id: "session-a"},
		{id: "session-b"},
		{id: "session-c"},
	}
	pids := map[string]int{
		"session-a": 111,
		"session-c": 333,
	}

	got := attachRunningPIDs(entries, pids)

	want := map[string]int{"session-a": 111, "session-b": 0, "session-c": 333}
	for _, e := range got {
		if e.pid != want[e.id] {
			t.Errorf("entry %s: pid = %d, want %d", e.id, e.pid, want[e.id])
		}
	}
}
