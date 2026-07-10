package views

import "testing"

func TestArtifactCSSEmbedded(t *testing.T) {
	if len(ArtifactCSS) < 1000 {
		t.Fatalf("artifact.css looks empty (%d bytes)", len(ArtifactCSS))
	}
	for _, sel := range []string{".page", ".corner", ".live-dot", ".work-group", ".glyph", ".rounds"} {
		if !contains(ArtifactCSS, sel) {
			t.Errorf("artifact.css missing selector %q", sel)
		}
	}
}

func contains(hay, needle string) bool { return len(hay) >= len(needle) && (indexOf(hay, needle) >= 0) }
func indexOf(hay, needle string) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
