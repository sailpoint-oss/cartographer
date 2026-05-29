package authscope

import (
	"reflect"
	"testing"
)

func TestSplitTokens(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"alpha", []string{"alpha"}},
		{"alpha,beta", []string{"alpha", "beta"}},
		{"alpha, beta ,gamma", []string{"alpha", "beta", "gamma"}},
		{"alpha beta\tgamma", []string{"alpha", "beta", "gamma"}},
		{"alpha;beta;gamma", []string{"alpha", "beta", "gamma"}},
	}
	for _, tc := range cases {
		got := SplitTokens(tc.in)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("SplitTokens(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestIsColonDelimitedAuthID(t *testing.T) {
	cases := map[string]bool{
		"api:orders:read":     true,
		"identity:users:read": true,
		"alpha":               false,
		"a:b":                 false,
		"api:orders":          false,
		"":                    false,
	}
	for in, want := range cases {
		if got := IsColonDelimitedAuthID(in); got != want {
			t.Errorf("IsColonDelimitedAuthID(%q) = %v, want %v", in, got, want)
		}
	}
}
