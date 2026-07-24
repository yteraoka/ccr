package ccr

import (
	"strings"
	"testing"
	"time"
)

func TestPreviewViewServingURLAfterEnded(t *testing.T) {
	end := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	got := previewView("/tmp/proj", 123, "", nil, time.Time{}, end, "http://localhost:8000/abc", nil, 20, 100)

	lines := strings.Split(got, "\n")
	endIdx, servingIdx := -1, -1
	for i, l := range lines {
		if strings.HasPrefix(l, "Ended:") {
			endIdx = i
		}
		if strings.HasPrefix(l, "Serving at:") {
			servingIdx = i
		}
	}
	if endIdx == -1 {
		t.Fatalf("no Ended: line found in %q", got)
	}
	if servingIdx != endIdx+1 {
		t.Errorf("Serving at: line at %d, want immediately after Ended: at %d\noutput:\n%s", servingIdx, endIdx, got)
	}
	if !strings.Contains(got, "Serving at: http://localhost:8000/abc") {
		t.Errorf("expected exact URL in output: %s", got)
	}
}

func TestPreviewViewNoServingURL(t *testing.T) {
	got := previewView("/tmp/proj", 123, "", nil, time.Time{}, time.Time{}, "", nil, 20, 100)
	if strings.Contains(got, "Serving at:") {
		t.Errorf("did not expect a Serving at: line when servingURL is empty: %s", got)
	}
}

func TestListViewKeyLegendIsLastLine(t *testing.T) {
	sessions := []sessionEntry{{id: "abc"}, {id: "def"}}
	got := listView(sessions, 0, 10, 100)

	lines := strings.Split(got, "\n")
	if len(lines) != 10 {
		t.Fatalf("got %d lines, want 10 (height budget): %q", len(lines), got)
	}
	last := lines[len(lines)-1]
	if !strings.Contains(last, "move") || !strings.Contains(last, "quit") {
		t.Errorf("last line = %q, want it to contain the key legend", last)
	}
}
