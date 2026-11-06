package picker

import "testing"

func TestPreviewColsForMatchesPane(t *testing.T) {
	cases := []struct {
		name      string
		width     int
		paneWidth int
		want      int
	}{
		{"unknown pane keeps heuristic", 190, 0, 130},
		{"narrow pane fits exactly", 190, 60, 61},
		{"wide pane takes all the room", 190, 212, 130},
		{"wide terminal fits the pane", 300, 212, 213},
		{"narrow terminal has no preview", 60, 120, 0},
		{"pane exactly the inner width", 190, 129, 130},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := previewColsFor(tc.width, tc.paneWidth); got != tc.want {
				t.Fatalf("previewColsFor(%d, %d) = %d, want %d", tc.width, tc.paneWidth, got, tc.want)
			}
		})
	}
}

func TestPreviewInnerWidthMatchesPane(t *testing.T) {
	if got := previewInnerWidth(previewColsFor(190, 60)); got != 60 {
		t.Fatalf("preview inner width = %d, want the pane width 60", got)
	}
}
