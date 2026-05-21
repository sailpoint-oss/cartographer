package javaextract

import "testing"

func TestResolveHeaderConstantName(t *testing.T) {
	constants := map[string]string{
		"TOTAL_COUNT_HEADER": "X-Total-Count",
		"POLL_HEADER":        "X-Poll-Interval",
	}
	cases := []struct {
		raw  string
		want string
	}{
		{`"X-Custom"`, "X-Custom"},
		{"TOTAL_COUNT_HEADER", "X-Total-Count"},
		{"ApiHeaders.TOTAL_COUNT_HEADER", "X-Total-Count"},
		{"SomeType.TOTAL_COUNT", "X-Total-Count"},
		{"SomeType.POLL_INTERVAL", "X-Poll-Interval"},
		{"UNKNOWN", ""},
	}
	for _, tc := range cases {
		if got := resolveHeaderConstantName(tc.raw, constants); got != tc.want {
			t.Errorf("resolveHeaderConstantName(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestMergeDelegatedResponseAnalysis(t *testing.T) {
	ctx := &extractContext{
		methodBodies: map[string]map[string]string{
			"ListBaseResource": {
				"okWithCount": `return Response.ok(items).header(TOTAL_COUNT_HEADER, String.valueOf(items.size())).build();`,
			},
		},
		constants: map[string]string{
			"TOTAL_COUNT_HEADER": "X-Total-Count",
		},
	}
	body := `return okWithCount(items);`
	headers, status := mergeDelegatedResponseAnalysis(body, "CatalogResource", "ListBaseResource", ctx, ctx.constants)
	if status != 0 {
		t.Errorf("status = %d, want 0", status)
	}
	if headers == nil || headers["X-Total-Count"] == "" {
		t.Fatalf("expected delegated X-Total-Count header, got %v", headers)
	}
}

func TestMethodDeclHasHTTPMapping(t *testing.T) {
	src := `package com.example;
public class Demo {
    @GetMapping("/x")
    public void mapped() {}
    public void plain() {}
}`
	dir := t.TempDir()
	writeTestFile(t, dir, "Demo.java", src)
	result, err := Extract(Config{RootDir: dir, SourceDirs: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Diagnostics["unmappedHandlerMethods"] == nil {
		t.Log("unmappedHandlerMethods not set when zero")
	}
}

func TestExtractResponseHeadersFromTextConstArg(t *testing.T) {
	body := `return Response.ok(list).header(TOTAL_COUNT_HEADER, count).build();`
	constants := map[string]string{"TOTAL_COUNT_HEADER": "X-Total-Count"}
	headers := extractResponseHeadersFromText(body, constants)
	if headers["X-Total-Count"] == "" {
		t.Fatalf("expected resolved const header, got %v", headers)
	}
}
