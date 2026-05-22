package extraction

import (
	"os"
	"path/filepath"
	"strings"
)

// applyCoLocatedOpenAPIMerge merges path operations from supplementary in-repo
// OpenAPI fragments when MergeCoLocatedOpenAPI is enabled. Fragment responses
// with typed application/json schemas are copied into the extracted spec when
// the extracted operation lacks typed 2xx on the same path/method.
func applyCoLocatedOpenAPIMerge(root string, specMap map[string]interface{}) {
	for _, candidate := range coLocatedOpenAPICandidates(root) {
		frag, err := LoadCanonicalSpec(candidate, root)
		if err != nil || frag == nil || frag.SpecMap == nil {
			continue
		}
		mergeFragmentResponses(specMap, frag.SpecMap)
	}
}

func coLocatedOpenAPICandidates(root string) []string {
	var out []string
	primary := firstCanonicalSpecPath(root)
	dirs := []string{
		filepath.Join(root, "api"),
		filepath.Join(root, "openapi"),
		filepath.Join(root, "src", "main", "resources", "openapi"),
		filepath.Join(root, "docs"),
	}
	names := []string{"openapi.yaml", "openapi.yml", "paths.yaml", "paths.yml", "api.yaml"}
	for _, dir := range dirs {
		for _, name := range names {
			p := filepath.Join(dir, name)
			if p == primary {
				continue
			}
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				out = append(out, p)
			}
		}
	}
	return out
}

func mergeFragmentResponses(into, fragment map[string]interface{}) {
	intoPaths := getMap(into, "paths")
	fragPaths := getMap(fragment, "paths")
	if intoPaths == nil || fragPaths == nil {
		return
	}
	for path, rawItem := range fragPaths {
		item, ok := rawItem.(map[string]interface{})
		if !ok {
			continue
		}
		for method, rawOp := range item {
			if !isHTTPMethod(method) {
				continue
			}
			fragOp, _ := rawOp.(map[string]interface{})
			if fragOp == nil {
				continue
			}
			intoItem := getMap(intoPaths, path)
			if intoItem == nil {
				intoItem = map[string]interface{}{}
				intoPaths[path] = intoItem
			}
			intoOp, _ := intoItem[method].(map[string]interface{})
			if intoOp == nil {
				intoItem[method] = fragOp
				continue
			}
			copyFragmentTyped2xx(intoOp, fragOp)
		}
	}
}

func copyFragmentTyped2xx(intoOp, fragOp map[string]interface{}) {
	if hasTypedJSON2xx(intoOp) {
		return
	}
	intoResp := getMap(intoOp, "responses")
	fragResp := getMap(fragOp, "responses")
	if fragResp == nil {
		return
	}
	if intoResp == nil {
		intoResp = map[string]interface{}{}
		intoOp["responses"] = intoResp
	}
	for status, raw := range fragResp {
		if len(status) != 3 || status[0] != '2' {
			continue
		}
		fragR, _ := raw.(map[string]interface{})
		if fragR == nil || !hasTypedJSONResponse(fragR) {
			continue
		}
		if intoResp[status] == nil {
			intoResp[status] = raw
		} else {
			intoR, _ := intoResp[status].(map[string]interface{})
			if intoR == nil {
				continue
			}
			content := getMap(fragR, "content")
			if content != nil {
				if intoR["content"] == nil {
					intoR["content"] = content
				} else {
					intoContent := getMap(intoR, "content")
					for k, v := range content {
						intoContent[k] = v
					}
				}
			}
		}
	}
}

func hasTypedJSON2xx(op map[string]interface{}) bool {
	resp := getMap(op, "responses")
	for status, raw := range resp {
		if len(status) != 3 || status[0] != '2' {
			continue
		}
		r, _ := raw.(map[string]interface{})
		if hasTypedJSONResponse(r) {
			return true
		}
	}
	return false
}

func hasTypedJSONResponse(resp map[string]interface{}) bool {
	if resp == nil {
		return false
	}
	content := getMap(resp, "content")
	jsonContent := getMap(content, "application/json")
	schema := getMap(jsonContent, "schema")
	if schema == nil {
		return false
	}
	if ref, ok := schema["$ref"].(string); ok && strings.HasPrefix(ref, "#/components/") {
		return true
	}
	return len(getMap(schema, "properties")) > 0
}

func getMap(parent map[string]interface{}, key string) map[string]interface{} {
	if parent == nil {
		return nil
	}
	v, ok := parent[key]
	if !ok || v == nil {
		return nil
	}
	m, _ := v.(map[string]interface{})
	return m
}

func isHTTPMethod(m string) bool {
	switch strings.ToLower(m) {
	case "get", "post", "put", "patch", "delete", "head", "options", "trace":
		return true
	default:
		return false
	}
}
