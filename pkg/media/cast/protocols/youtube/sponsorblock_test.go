package youtube

import "testing"

func TestATrackWaitingOnAJumpHandsOverNothing(t *testing.T) {
	tr := &track{jumping: true}
	if n, err := tr.Read(t.Context(), make([]int16, 64)); n != 0 || err != nil {
		t.Errorf("read %d, %v", n, err)
	}
}
