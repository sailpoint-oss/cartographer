package extractionopts

import "testing"

func TestSignaturePaginationSet(t *testing.T) {
	opts := Options{
		SignaturePaginationTypes: []string{" QueryOptions ", "", "PageRequest"},
	}
	set := opts.SignaturePaginationSet()
	if !set["QueryOptions"] || !set["PageRequest"] {
		t.Fatalf("unexpected set: %#v", set)
	}
	if set[""] {
		t.Error("empty names should be skipped")
	}
}

func TestOptionsZeroValue(t *testing.T) {
	if got := (Options{}).SignaturePaginationSet(); len(got) != 0 {
		t.Fatalf("zero options should yield empty set, got %#v", got)
	}
}
