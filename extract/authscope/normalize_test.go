package authscope

import "testing"

func TestNormalizeSpringRights(t *testing.T) {
	in := []string{"ROLE_api:example:read", "ROLE_ADMIN", "api:foo:bar"}
	got := NormalizeSpringRights(in)
	if got[0] != "api:example:read" {
		t.Fatalf("got[0] = %q", got[0])
	}
	if got[1] != "ROLE_ADMIN" {
		t.Fatalf("got[1] = %q", got[1])
	}
}
