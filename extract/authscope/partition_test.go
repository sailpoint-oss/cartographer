package authscope

import "testing"

func TestPartitionTokensWithoutMapping(t *testing.T) {
	rights, scopes := PartitionTokens([]string{"api:orders:read", "read"}, nil)
	if len(rights) != 1 || rights[0] != "api:orders:read" {
		t.Fatalf("rights = %v", rights)
	}
	if len(scopes) != 1 || scopes[0] != "read" {
		t.Fatalf("scopes = %v", scopes)
	}
}
