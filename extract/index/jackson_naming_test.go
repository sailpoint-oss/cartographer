package index

import "testing"

func TestParseJacksonNamingStrategy(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"(SnakeCaseStrategy.class)", "snake_case"},
		{"(PropertyNamingStrategies.SnakeCaseStrategy.class)", "snake_case"},
		{"(UpperCamelCaseStrategy.class)", "upper_camel"},
		{"(LowerCaseStrategy.class)", "lower_case"},
		{"(KebabCaseStrategy.class)", "kebab_case"},
		{"(LowerCamelCaseStrategy.class)", "lower_camel"},
		{"()", ""},
	}
	for _, tc := range tests {
		if got := parseJacksonNamingStrategy(tc.in); got != tc.want {
			t.Errorf("parseJacksonNamingStrategy(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestApplyJacksonNaming(t *testing.T) {
	tests := []struct {
		field    string
		strategy string
		want     string
	}{
		{"userName", "snake_case", "user_name"},
		{"userId", "snake_case", "user_id"},
		{"userName", "kebab_case", "user-name"},
		{"userName", "lower_case", "username"},
		{"user_name", "upper_camel", "UserName"},
		{"userName", "lower_camel", "userName"},
		{"UserName", "lower_camel", "userName"},
	}
	for _, tc := range tests {
		if got := applyJacksonNaming(tc.field, tc.strategy); got != tc.want {
			t.Errorf("applyJacksonNaming(%q, %q) = %q, want %q", tc.field, tc.strategy, got, tc.want)
		}
	}
}

func TestApplyClassJacksonNamingRespectsExplicitJsonProperty(t *testing.T) {
	fields := []FieldDecl{
		{Name: "userName", JSONName: "userName", Annotations: map[string]string{}},
		{Name: "customId", JSONName: "custom_id", Annotations: map[string]string{"JsonProperty": `("custom_id")`}},
	}
	applyClassJacksonNaming(fields, "snake_case")
	if fields[0].JSONName != "user_name" {
		t.Fatalf("expected snake_case wire name, got %q", fields[0].JSONName)
	}
	if fields[1].JSONName != "custom_id" {
		t.Fatalf("explicit @JsonProperty must win, got %q", fields[1].JSONName)
	}
}
