package product

import "testing"

func TestAgentAndPaths(t *testing.T) {
	c := Context{
		Name:     "LANovo",
		Version:  "0.0.7",
		Site:     "github.com/ygelfand/LANovo",
		StateDir: "/data/misc/lanovo",
	}
	if got, want := c.Agent(), "(LANovo 0.0.7, github.com/ygelfand/LANovo)"; got != want {
		t.Errorf("Agent() = %q, want %q", got, want)
	}
	if got, want := c.Path("tiles"), "/data/misc/lanovo/tiles"; got != want {
		t.Errorf("Path(tiles) = %q, want %q", got, want)
	}
}
