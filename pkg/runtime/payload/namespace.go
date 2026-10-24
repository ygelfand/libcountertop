package payload

import (
	"errors"
	"slices"
)

const marker = "__payload_namespace"

type Bind struct {
	Source string
	Target string
}

func namespaceArgs(binds []Bind, path string, args []string) []string {
	out := []string{marker}
	for _, b := range binds {
		out = append(out, b.Source, b.Target)
	}
	out = append(out, "--", path)
	return append(out, args...)
}

func parseNamespace(args []string) ([]Bind, []string, error) {
	sep := slices.Index(args, "--")
	if sep < 0 || sep%2 != 0 || sep == len(args)-1 {
		return nil, nil, errors.New("payload: malformed namespace arguments")
	}
	binds := make([]Bind, 0, sep/2)
	for i := 0; i < sep; i += 2 {
		binds = append(binds, Bind{Source: args[i], Target: args[i+1]})
	}
	return binds, args[sep+1:], nil
}
