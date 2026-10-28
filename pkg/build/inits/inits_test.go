package inits

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnInitWritingAnotherFilesVariableIsFound(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "list.go", "package p\n\nvar list []int\n\nvar own = map[string]int{}\n")
	write(t, dir, "add.go", "package p\n\nfunc init() {\n\tlist = append(list, 1)\n}\n")
	write(
		t,
		dir,
		"own.go",
		"package p\n\nvar mine []int\n\nfunc init() {\n\tmine = append(mine, 1)\n\tx := 2\n\t_ = x\n}\n",
	)
	write(t, dir, "index.go", "package p\n\nfunc init() {\n\town[\"a\"] = 1\n}\n")
	write(
		t,
		dir,
		"call.go",
		"package p\n\nfunc init() {\n\tregister(list)\n}\n\nfunc register([]int) {}\n",
	)

	found, err := Scattered(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"add.go:4: init writes list, declared in list.go",
		"index.go:4: init writes own, declared in list.go",
	}
	if strings.Join(found, "\n") != strings.Join(want, "\n") {
		t.Errorf("found:\n%s\nwant:\n%s", strings.Join(found, "\n"), strings.Join(want, "\n"))
	}
}

func TestTheLibraryHasNoScatteredInits(t *testing.T) {
	found, err := Scattered("../../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range found {
		t.Error(f)
	}
}

func write(t *testing.T, dir, name, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}
