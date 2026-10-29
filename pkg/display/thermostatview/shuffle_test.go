package thermostatview

import (
	"testing"
	"time"
)

func TestANewNumberShufflesThenSettles(t *testing.T) {
	v := &View{shuffles: map[string]shuffle{}}
	at := time.Unix(1000, 0)
	if got, moving := v.settle("hall", "72", at); got != "72" || moving {
		t.Fatalf("first sight = %q moving %v, want 72 still", got, moving)
	}
	if _, moving := v.settle("hall", "73", at); !moving {
		t.Error("a changed number did not shuffle")
	}
	if got, moving := v.settle("hall", "73", at.Add(shuffleFor)); got != "73" || moving {
		t.Errorf("after the shuffle = %q moving %v, want 73 still", got, moving)
	}
	if got, _ := v.settle("hall", "73", at.Add(shuffleFor/2)); len(got) != 2 {
		t.Errorf("mid shuffle %q changed length", got)
	}
}
