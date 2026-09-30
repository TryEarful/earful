package templates

import "testing"

func TestVoiceIndexIsStableAndInRange(t *testing.T) {
	seen := map[int]bool{}
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p"} {
		s := SurveyView{ID: id}
		got := s.VoiceIndex()
		if got < 1 || got > 10 {
			t.Fatalf("VoiceIndex(%q) = %d, want 1 to 10", id, got)
		}
		if again := s.VoiceIndex(); again != got {
			t.Fatalf("VoiceIndex(%q) changed from %d to %d", id, got, again)
		}
		seen[got] = true
	}
	if len(seen) < 4 {
		t.Errorf("16 surveys drew only %d colours; the spread is too narrow", len(seen))
	}
}
