// Package extraction -- canonical.go handles in-repo OpenAPI/Swagger specs.
//
// Some services are the source of truth for their own OpenAPI spec: they
// either hand-author a file like `sailpoint-api.yaml`, or they generate one
// from code annotations (swaggo for Go, openapi-generator for Spring) and
// commit the result. For those services, running source-code extraction
// produces zero operations because the controllers simply extend generated
// interfaces or are non-existent. The canonical-spec passthrough detects
// these files and uses them directly.
//
// Supported layouts in priority order:
//
//  1. <root>/sailpoint-api.yaml
//  2. <root>/<svc>-api/sailpoint-api.yaml
//  3. <root>/openapi.yaml / openapi.yml / openapi.json
//  4. <root>/openapi-spec.yaml
//  5. <root>/docs/{openapi,swagger}.{yaml,yml,json}
//  6. <root>/api/openapi.{yaml,yml}
//  7. <root>/api-spec/openapi.{yaml,yml}
//  8. <root>/spec/openapi.{yaml,yml}
//
// Swagger 2.0 files (swaggo output) are auto-detected by the "swagger: 2.0"
// top-level field or the presence of "definitions:" without "openapi:" and
// are converted to OpenAPI 3.0 via an in-house structural converter so the
// rest of the toolchain always sees OpenAPI 3 shape.
//
// Parsing, validation, and ref bookkeeping delegate to Navigator, the
// workspace-wide OpenAPI tooling. kin-openapi is not used anywhere in this
// package.
package extraction

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	navigator "github.com/sailpoint-oss/navigator"
	"gopkg.in/yaml.v3"
)

// CanonicalSpec is a spec loaded from an in-repo file. Kind reports the
// source dialect so callers can log which conversion path ran.
type CanonicalSpec struct {
	// Path is the absolute on-disk path to the source file.
	Path string
	// RelPath is Path relative to RootDir when RootDir was passed to
	// DiscoverCanonicalSpec; empty otherwise.
	RelPath string
	// Kind is "openapi3" for native OpenAPI 3.x, "swagger2" for
	// Swagger 2.0 files that were converted.
	Kind string
	// SpecMap is the parsed spec as a nested map. Always in OpenAPI 3
	// shape -- Swagger 2 files are converted before returning.
	SpecMap map[string]interface{}
	// Operations is the total count of operations across all paths.
	Operations int
	// Types is the count of entries under components.schemas.
	Types int
}

// DiscoverCanonicalSpec searches for an in-repo spec under root. Returns
// (nil, nil) when no candidate is found; callers should treat that as "fall
// back to source extraction". Returns (spec, nil) when a candidate is found
// and successfully loaded. Returns (nil, err) only for the rare case where a
// candidate exists but is unreadable / malformed; callers typically log and
// continue.
func DiscoverCanonicalSpec(root string) (*CanonicalSpec, error) {
	candidate := firstCanonicalSpecPath(root)
	if candidate == "" {
		return nil, nil
	}
	return LoadCanonicalSpec(candidate, root)
}

// LoadCanonicalSpec parses a spec file and converts from Swagger 2 when
// necessary. relRoot is optional; when non-empty the returned RelPath is
// computed relative to it for cleaner logs.
//
// For OpenAPI 3 documents the map-walk bundler resolves external $ref
// pointers (e.g. ./paths/widgets.yaml) so the persisted output is a single
// self-contained document; this is essential for services that split their
// spec across files, such as the openapi-generator Spring convention used
// by gov-certification.
func LoadCanonicalSpec(path, relRoot string) (*CanonicalSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read canonical spec %s: %w", path, err)
	}
	specMap, kind, ops, types, err := parseAndResolveSpec(path, data)
	if err != nil {
		return nil, fmt.Errorf("parse canonical spec %s: %w", path, err)
	}
	rel := ""
	if relRoot != "" {
		if rp, rerr := filepath.Rel(relRoot, path); rerr == nil {
			rel = rp
		}
	}
	return &CanonicalSpec{
		Path:       path,
		RelPath:    rel,
		Kind:       kind,
		SpecMap:    specMap,
		Operations: ops,
		Types:      types,
	}, nil
}

// parseAndResolveSpec loads the file, detects the dialect, bundles
// external $refs for OpenAPI 3 multi-file layouts, and counts operations
// and component schemas via Navigator so the toolchain sees identical
// numbers whether it parses the spec itself or consumes the persisted
// canonical copy.
func parseAndResolveSpec(path string, data []byte) (map[string]any, string, int, int, error) {
	specMap, kind, err := parseAndNormalizeSpec(data)
	if err != nil {
		return nil, "", 0, 0, err
	}

	// Swagger 2 specs are self-contained after the structural
	// converter runs, so we skip the multi-file bundler.
	if kind == "openapi3" {
		specMap = bundleOpenAPI3(path, specMap)
	}

	ops, types := countViaNavigator(specMap)
	return specMap, kind, ops, types, nil
}

// bundleOpenAPI3 inlines path-item refs and hoists schema refs using the
// in-house map-walk inliner. On any error we fall back to the un-bundled
// map rather than propagate -- downstream is fine with partial specs and
// meridian will still persist something callers can inspect.
func bundleOpenAPI3(path string, specMap map[string]any) map[string]any {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return specMap
	}
	inliner := newRefInliner(absPath)
	resolved, err := inliner.inline(specMap, absPath)
	if err != nil {
		return specMap
	}
	out, ok := resolved.(map[string]any)
	if !ok {
		return specMap
	}
	inliner.mergeComponents(out)
	return out
}

// countViaNavigator parses the bundled map with Navigator and returns
// (operations, component-schemas). Navigator is the canonical parser for
// OpenAPI in this toolchain, so delegating here keeps the counts we
// persist in line with what meridian / telescope see.
func countViaNavigator(spec map[string]any) (int, int) {
	data, err := json.Marshal(spec)
	if err != nil {
		return countOperations(spec), countSchemas(spec)
	}
	idx := navigator.Parse(data)
	if idx == nil {
		// Fall back to the raw map-walk counts when Navigator cannot
		// parse the bundled bytes (e.g. a non-mapping root).
		return countOperations(spec), countSchemas(spec)
	}
	ops := 0
	for _, list := range idx.OperationsByPath {
		ops += len(list)
	}
	return ops, len(idx.Schemas)
}

// isExternalRef reports whether ref points outside the current doc.
// Intra-document refs start with "#" and point to #/components/... --
// those we want to keep as-is so the flattened spec stays compact.
func isExternalRef(ref string) bool {
	if ref == "" {
		return false
	}
	return !strings.HasPrefix(ref, "#")
}

// refInliner walks a decoded OpenAPI map and inlines external $refs.
// It has two modes depending on the target file's role:
//
//  1. PathItem files (paths/*.yaml) are inlined in place because
//     OpenAPI path-item content isn't reusable across multiple paths.
//  2. Schema/parameter/response files are hoisted into components
//     under a derived name and all their external $refs are rewritten
//     to intra-document #/components/schemas/<name> pointers.
//
// This matches what `redocly bundle` and `swagger-cli bundle`
// produce, breaks file-level cycles cleanly, and keeps the bundled
// spec readable when downstream tools don't follow external refs.
type refInliner struct {
	rootPath string
	// cache of parsed file contents keyed by absolute path.
	fileCache map[string]map[string]any
	// components lifted from external schema files: componentName ->
	// inlined content. Written back into the root document in
	// mergeComponents(). refToName maps absRefTarget to componentName.
	liftedSchemas map[string]any
	refToName     map[string]string
	nameInUse     map[string]struct{}
}

func newRefInliner(rootAbs string) *refInliner {
	return &refInliner{
		rootPath:      rootAbs,
		fileCache:     map[string]map[string]any{},
		liftedSchemas: map[string]any{},
		refToName:     map[string]string{},
		nameInUse:     map[string]struct{}{},
	}
}

func (r *refInliner) inline(node any, currentFile string) (any, error) {
	return r.inlineWith(node, currentFile, map[string]struct{}{})
}

func (r *refInliner) inlineWith(node any, currentFile string, inflight map[string]struct{}) (any, error) {
	switch n := node.(type) {
	case map[string]any:
		if refVal, ok := n["$ref"].(string); ok && isExternalRef(refVal) {
			return r.resolveRef(refVal, currentFile, inflight)
		}
		out := make(map[string]any, len(n))
		for k, v := range n {
			inlined, err := r.inlineWith(v, currentFile, inflight)
			if err != nil {
				return nil, err
			}
			out[k] = inlined
		}
		return out, nil
	case []any:
		out := make([]any, len(n))
		for i, v := range n {
			inlined, err := r.inlineWith(v, currentFile, inflight)
			if err != nil {
				return nil, err
			}
			out[i] = inlined
		}
		return out, nil
	default:
		return n, nil
	}
}

// resolveRef decides whether to inline the target in place or to
// hoist it into components. Target-file URIs are normalised via
// Navigator's URI helpers so the behaviour matches the rest of the
// toolchain (same file/fragment splitting as telescope / meridian).
//
// The hoist branch is cycle-safe because the lifted component keeps
// its intra-document $ref intact, so a self-referencing schema stays
// compact (one entry in components.schemas) and only the $ref target
// changes from "./Criteria.yaml" to "#/components/schemas/Criteria".
func (r *refInliner) resolveRef(ref, currentFile string, inflight map[string]struct{}) (any, error) {
	filePart, fragment := navigator.SplitRefURI(ref)
	if filePart == "" {
		return map[string]any{"$ref": ref}, nil
	}
	currentURI := navigator.PathToURI(currentFile)
	targetURI := navigator.ResolveRelativeURI(currentURI, filePart)
	target := navigator.URIToPath(targetURI)

	if isPathItemRef(target) {
		return r.inlinePathItem(ref, target, fragment, inflight)
	}
	return r.hoistSchemaRef(ref, target, fragment)
}

// inlinePathItem loads the file and inlines its contents recursively.
// File cycles are still bounded by inflight since PathItem files are
// rarely self-recursive in practice.
func (r *refInliner) inlinePathItem(ref, target, fragment string, inflight map[string]struct{}) (any, error) {
	if _, cycle := inflight[target]; cycle {
		return map[string]any{"$ref": ref}, nil
	}
	doc, err := r.loadFile(target)
	if err != nil {
		return nil, err
	}
	inflight[target] = struct{}{}
	defer delete(inflight, target)

	var sub any = doc
	if fragment != "" {
		sub, err = pointerDeref(doc, fragment)
		if err != nil {
			return nil, fmt.Errorf("ref %s: %w", ref, err)
		}
	}
	return r.inlineWith(sub, target, inflight)
}

// hoistSchemaRef lifts the target into components and returns an
// intra-document $ref. Subsequent refs to the same file reuse the
// cached component name. The component's own content is inlined
// *once*, and recursive refs inside it that resolve back to the same
// file turn into #/components/schemas/<name> via this same code path.
func (r *refInliner) hoistSchemaRef(ref, target, fragment string) (any, error) {
	if existing, ok := r.refToName[target]; ok {
		return map[string]any{"$ref": componentRef(existing, fragment)}, nil
	}
	name := r.uniqueComponentName(target, fragment)
	r.refToName[target] = name
	r.nameInUse[name] = struct{}{}

	doc, err := r.loadFile(target)
	if err != nil {
		return nil, err
	}
	var sub any = doc
	if fragment != "" {
		sub, err = pointerDeref(doc, fragment)
		if err != nil {
			return nil, fmt.Errorf("ref %s: %w", ref, err)
		}
	}
	inlined, err := r.inlineWith(sub, target, map[string]struct{}{})
	if err != nil {
		return nil, err
	}
	r.liftedSchemas[name] = inlined
	return map[string]any{"$ref": componentRef(name, fragment)}, nil
}

// mergeComponents writes every lifted schema into root.components.schemas.
// If a name collides with an existing entry we keep the original; the
// uniqueComponentName logic should have avoided that case.
func (r *refInliner) mergeComponents(root map[string]any) {
	if len(r.liftedSchemas) == 0 {
		return
	}
	components, _ := root["components"].(map[string]any)
	if components == nil {
		components = map[string]any{}
		root["components"] = components
	}
	schemas, _ := components["schemas"].(map[string]any)
	if schemas == nil {
		schemas = map[string]any{}
		components["schemas"] = schemas
	}
	for name, value := range r.liftedSchemas {
		if _, clash := schemas[name]; clash {
			// Collision: prefer the original entry, skip the lifted one.
			continue
		}
		schemas[name] = value
	}
}

// uniqueComponentName derives a stable, unique name from the target
// file basename. For fragments we append a suffix derived from the
// fragment so different anchors in the same file get distinct names.
func (r *refInliner) uniqueComponentName(target, fragment string) string {
	base := strings.TrimSuffix(filepath.Base(target), filepath.Ext(target))
	base = sanitizeComponentName(base)
	if fragment != "" {
		parts := strings.Split(strings.TrimPrefix(fragment, "/"), "/")
		if len(parts) > 0 {
			base = base + "_" + sanitizeComponentName(parts[len(parts)-1])
		}
	}
	name := base
	for i := 2; ; i++ {
		if _, clash := r.nameInUse[name]; !clash {
			return name
		}
		name = fmt.Sprintf("%s_%d", base, i)
	}
}

// sanitizeComponentName strips characters that aren't safe in a
// component key. OpenAPI allows `[a-zA-Z0-9._-]` in keys.
func sanitizeComponentName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if out == "" {
		out = "External"
	}
	return out
}

// componentRef produces the intra-document pointer for a lifted
// component. The fragment is ignored -- we've already dereferenced
// it when lifting -- so all lifted content is addressable at the top
// level of components.schemas.
func componentRef(name, _ string) string {
	return "#/components/schemas/" + name
}

// isPathItemRef identifies files that hold an OpenAPI PathItem rather
// than a schema. Convention in our repos: any file under a path
// segment literally named "paths" is treated as a path item.
func isPathItemRef(target string) bool {
	clean := filepath.ToSlash(target)
	return strings.Contains(clean, "/paths/")
}

func (r *refInliner) loadFile(path string) (map[string]any, error) {
	if cached, ok := r.fileCache[path]; ok {
		return cached, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var doc map[string]any
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".json" {
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	} else {
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	if doc == nil {
		doc = map[string]any{}
	}
	// yaml.v3 decodes mappings as map[string]any by default; numeric
	// keys and arrays of maps may come back as map[interface{}]interface{}
	// in some paths. Normalise to map[string]any so json.Marshal works.
	doc = normaliseMap(doc)
	r.fileCache[path] = doc
	return doc, nil
}

// normaliseMap recursively converts map[interface{}]interface{} values
// to map[string]any so the decoded YAML tree can round-trip through
// encoding/json without failures on interface-keyed maps.
func normaliseMap(m map[string]any) map[string]any {
	for k, v := range m {
		m[k] = normaliseValue(v)
	}
	return m
}

func normaliseValue(v any) any {
	switch vv := v.(type) {
	case map[string]any:
		return normaliseMap(vv)
	case map[any]any:
		out := make(map[string]any, len(vv))
		for k, val := range vv {
			out[fmt.Sprintf("%v", k)] = normaliseValue(val)
		}
		return out
	case []any:
		for i, item := range vv {
			vv[i] = normaliseValue(item)
		}
		return vv
	default:
		return v
	}
}

// pointerDeref walks a JSON-pointer fragment (e.g. /components/schemas/Foo)
// against doc and returns the sub-value. Follows RFC 6901 escape rules:
// "~1" -> "/" and "~0" -> "~".
func pointerDeref(doc map[string]any, fragment string) (any, error) {
	fragment = strings.TrimPrefix(fragment, "#")
	fragment = strings.TrimPrefix(fragment, "/")
	if fragment == "" {
		return doc, nil
	}
	parts := strings.Split(fragment, "/")
	var cur any = doc
	for _, raw := range parts {
		tok := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		switch c := cur.(type) {
		case map[string]any:
			v, ok := c[tok]
			if !ok {
				return nil, fmt.Errorf("fragment segment %q not found", tok)
			}
			cur = v
		case []any:
			// Numeric index; parse best-effort.
			idx := 0
			for _, r := range tok {
				if r < '0' || r > '9' {
					return nil, fmt.Errorf("non-numeric array index %q", tok)
				}
				idx = idx*10 + int(r-'0')
			}
			if idx < 0 || idx >= len(c) {
				return nil, fmt.Errorf("array index %d out of range", idx)
			}
			cur = c[idx]
		default:
			return nil, fmt.Errorf("cannot descend into %T at %q", cur, tok)
		}
	}
	return cur, nil
}

// canonicalSpecCandidates returns the ordered search list for root. Exposed
// for tests so they can assert the priority order without duplicating it.
func canonicalSpecCandidates(root string) []string {
	// Root-level candidates (highest priority).
	rel := []string{
		"sailpoint-api.yaml",
		"sailpoint-api.yml",
		"openapi.yaml",
		"openapi.yml",
		"openapi.json",
		"openapi-spec.yaml",
		"openapi-spec.yml",
		filepath.Join("docs", "openapi.yaml"),
		filepath.Join("docs", "openapi.yml"),
		filepath.Join("docs", "openapi.json"),
		filepath.Join("docs", "openapi-spec.yaml"),
		filepath.Join("docs", "openapi-spec.yml"),
		filepath.Join("docs", "swagger.yaml"),
		filepath.Join("docs", "swagger.yml"),
		filepath.Join("docs", "swagger.json"),
		filepath.Join("api", "openapi.yaml"),
		filepath.Join("api", "openapi.yml"),
		filepath.Join("api-spec", "openapi.yaml"),
		filepath.Join("api-spec", "openapi.yml"),
		filepath.Join("spec", "openapi.yaml"),
		filepath.Join("spec", "openapi.yml"),
	}
	out := make([]string, 0, len(rel)+4)
	for _, r := range rel {
		out = append(out, filepath.Join(root, r))
	}

	// openapi-generator split-module convention: <root>/<svc>-api/sailpoint-api.yaml
	// (gov-certification → gov-certification-api/sailpoint-api.yaml). We scan
	// only one level deep to avoid runaway recursion in large monorepos.
	if entries, err := os.ReadDir(root); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(name, "-api") && !strings.HasSuffix(name, "_api") {
				continue
			}
			sub := filepath.Join(root, name)
			for _, tail := range []string{"sailpoint-api.yaml", "sailpoint-api.yml", "openapi.yaml", "openapi.yml"} {
				out = append(out, filepath.Join(sub, tail))
			}
		}
	}
	return out
}

func firstCanonicalSpecPath(root string) string {
	for _, candidate := range canonicalSpecCandidates(root) {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

// parseAndNormalizeSpec loads a YAML or JSON spec and returns it as an
// OpenAPI 3 map. When the source is Swagger 2.0 the in-house structural
// converter is used. Returns ("", err) when the format is unrecognised so
// callers can log the exact reason.
func parseAndNormalizeSpec(data []byte) (map[string]interface{}, string, error) {
	var raw map[string]interface{}
	// Try YAML first (covers JSON too via yaml.v3's JSON-compat parsing).
	if err := yaml.Unmarshal(data, &raw); err != nil {
		// Fall back to strict JSON in case yaml parsing chokes on
		// something JSON-specific (rare, but keeps error messages
		// understandable).
		if jerr := json.Unmarshal(data, &raw); jerr != nil {
			return nil, "", fmt.Errorf("yaml: %v; json: %v", err, jerr)
		}
	}
	if raw == nil {
		return nil, "", fmt.Errorf("spec is empty")
	}
	raw = normaliseMap(raw)

	if _, ok := raw["openapi"].(string); ok {
		return raw, "openapi3", nil
	}
	// Swagger 2.0 signals: top-level "swagger: 2.0" or the combination of
	// "definitions" + ("paths" | "basePath") without "openapi".
	if v, ok := raw["swagger"].(string); ok && strings.HasPrefix(v, "2.") {
		return convertSwagger2ToOpenAPI3(raw), "swagger2", nil
	}
	_, hasDefs := raw["definitions"]
	_, hasPaths := raw["paths"]
	_, hasBase := raw["basePath"]
	if hasDefs && (hasPaths || hasBase) {
		return convertSwagger2ToOpenAPI3(raw), "swagger2", nil
	}
	return nil, "", fmt.Errorf("unrecognised spec: no 'openapi' or 'swagger' top-level field")
}

// convertSwagger2ToOpenAPI3 performs a structural OpenAPI 2.0 → 3.0
// conversion over a decoded YAML map. It targets the shapes we actually
// see in the fleet (swaggo output from Go services, openapi-generator
// Spring output) and aims to produce a self-contained OpenAPI 3.0 map
// with components.schemas populated so Navigator and the rest of the
// toolchain handle the document uniformly.
//
// The conversion is intentionally lossless for the fields that downstream
// stages inspect (info, paths, operations, parameters, responses,
// definitions, securityDefinitions) and tolerant of unknown fields
// (they pass through unchanged). Deep OAS 2 features that the toolchain
// does not consume (e.g. composite body/formData parameter shapes beyond
// the single-body case) are lowered to a best-effort OAS 3 equivalent
// rather than dropped.
func convertSwagger2ToOpenAPI3(raw map[string]any) map[string]any {
	out := map[string]any{"openapi": "3.0.0"}

	// info -- ensure title/version are populated so downstream validators
	// accept the converted document even when swaggo omitted them.
	info, _ := raw["info"].(map[string]any)
	if info == nil {
		info = map[string]any{}
	}
	if _, ok := info["title"]; !ok {
		info["title"] = "API"
	}
	if _, ok := info["version"]; !ok {
		info["version"] = "1.0.0"
	}
	out["info"] = info

	// servers -- built from host/basePath/schemes. When nothing is
	// specified we leave servers unset (callers often patch this later).
	if servers := buildServersFromSwagger2(raw); len(servers) > 0 {
		out["servers"] = servers
	}

	// Root consumes/produces act as defaults for every operation.
	rootConsumes := stringSlice(raw["consumes"])
	rootProduces := stringSlice(raw["produces"])

	// paths -- walk each operation, converting parameters, requestBody
	// and responses.
	if paths, ok := raw["paths"].(map[string]any); ok {
		out["paths"] = convertSwagger2Paths(paths, rootConsumes, rootProduces)
	} else {
		out["paths"] = map[string]any{}
	}

	// tags / externalDocs / security -- pass through unchanged (keys and
	// semantics are compatible with OpenAPI 3).
	if v, ok := raw["tags"]; ok {
		out["tags"] = v
	}
	if v, ok := raw["externalDocs"]; ok {
		out["externalDocs"] = v
	}
	if v, ok := raw["security"]; ok {
		out["security"] = v
	}

	// components.{schemas,parameters,responses,securitySchemes}.
	components := map[string]any{}
	if defs, ok := raw["definitions"].(map[string]any); ok && len(defs) > 0 {
		schemas := make(map[string]any, len(defs))
		for name, s := range defs {
			schemas[name] = convertSwagger2Schema(s)
		}
		components["schemas"] = schemas
	}
	if params, ok := raw["parameters"].(map[string]any); ok && len(params) > 0 {
		cparams := map[string]any{}
		for name, p := range params {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			if converted := convertSwagger2Parameter(pm); converted != nil {
				cparams[name] = converted
			}
		}
		if len(cparams) > 0 {
			components["parameters"] = cparams
		}
	}
	if resps, ok := raw["responses"].(map[string]any); ok && len(resps) > 0 {
		cresps := map[string]any{}
		for name, r := range resps {
			rm, ok := r.(map[string]any)
			if !ok {
				continue
			}
			cresps[name] = convertSwagger2Response(rm, rootProduces)
		}
		if len(cresps) > 0 {
			components["responses"] = cresps
		}
	}
	if sec, ok := raw["securityDefinitions"].(map[string]any); ok && len(sec) > 0 {
		ss := map[string]any{}
		for name, s := range sec {
			sm, ok := s.(map[string]any)
			if !ok {
				continue
			}
			ss[name] = convertSwagger2SecurityScheme(sm)
		}
		if len(ss) > 0 {
			components["securitySchemes"] = ss
		}
	}
	if len(components) > 0 {
		out["components"] = components
	}

	// Rewrite every $ref from Swagger 2 pointers to OpenAPI 3 ones.
	rewriteSwagger2Refs(out)

	// Preserve x-* extensions on the root.
	for k, v := range raw {
		if strings.HasPrefix(k, "x-") {
			out[k] = v
		}
	}
	return out
}

// buildServersFromSwagger2 assembles an OpenAPI 3 servers array from the
// OAS 2 host/basePath/schemes triplet. When only basePath is set we emit
// a single scheme-less entry ("/v1"), which is what meridian / scalar
// render for services whose host is the gateway anyway.
func buildServersFromSwagger2(raw map[string]any) []any {
	host, _ := raw["host"].(string)
	base, _ := raw["basePath"].(string)
	schemes := stringSlice(raw["schemes"])
	if host == "" && base == "" && len(schemes) == 0 {
		return nil
	}
	if len(schemes) == 0 {
		url := base
		if host != "" {
			url = host + base
		}
		if url == "" {
			return nil
		}
		return []any{map[string]any{"url": url}}
	}
	servers := make([]any, 0, len(schemes))
	for _, s := range schemes {
		url := base
		if host != "" {
			url = s + "://" + host + base
		}
		if url == "" {
			continue
		}
		servers = append(servers, map[string]any{"url": url})
	}
	return servers
}

// convertSwagger2Paths walks every path and rewrites each operation.
// Paths themselves are not structurally different (the /path: {get: {...},
// parameters: [...]} shape is the same) so we only need to transform the
// children.
func convertSwagger2Paths(paths map[string]any, rootConsumes, rootProduces []string) map[string]any {
	out := make(map[string]any, len(paths))
	methods := map[string]struct{}{
		"get": {}, "put": {}, "post": {}, "delete": {},
		"options": {}, "head": {}, "patch": {}, "trace": {},
	}
	for path, item := range paths {
		im, ok := item.(map[string]any)
		if !ok {
			out[path] = item
			continue
		}
		converted := map[string]any{}
		// Path-level parameters apply to every operation. We convert
		// them the same way as op-level params.
		var pathParams []any
		if raw, ok := im["parameters"].([]any); ok {
			pathParams = convertSwagger2ParameterList(raw)
		}
		for k, v := range im {
			kl := strings.ToLower(k)
			if _, isMethod := methods[kl]; isMethod {
				op, ok := v.(map[string]any)
				if !ok {
					continue
				}
				converted[kl] = convertSwagger2Operation(op, rootConsumes, rootProduces)
				continue
			}
			if kl == "parameters" {
				if len(pathParams) > 0 {
					converted["parameters"] = pathParams
				}
				continue
			}
			if k == "$ref" {
				converted[k] = v
				continue
			}
			converted[k] = v
		}
		out[path] = converted
	}
	return out
}

// convertSwagger2Operation splits an OAS 2 operation into its OAS 3
// equivalent. The headline transformation is `parameters[in=body]` →
// `requestBody` and response `schema` → `content[mediaType].schema`.
// formData parameters are coalesced into a single `multipart/form-data`
// request body because OAS 3 requires them to live on the requestBody
// rather than as loose parameters.
func convertSwagger2Operation(op map[string]any, rootConsumes, rootProduces []string) map[string]any {
	out := map[string]any{}
	consumes := stringSlice(op["consumes"])
	if len(consumes) == 0 {
		consumes = rootConsumes
	}
	if len(consumes) == 0 {
		consumes = []string{"application/json"}
	}
	produces := stringSlice(op["produces"])
	if len(produces) == 0 {
		produces = rootProduces
	}
	if len(produces) == 0 {
		produces = []string{"application/json"}
	}

	var keptParams []any
	var bodyParam map[string]any
	var formDataParams []map[string]any
	if raw, ok := op["parameters"].([]any); ok {
		for _, p := range raw {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			in, _ := pm["in"].(string)
			switch in {
			case "body":
				bodyParam = pm
			case "formData":
				formDataParams = append(formDataParams, pm)
			default:
				if converted := convertSwagger2Parameter(pm); converted != nil {
					keptParams = append(keptParams, converted)
				}
			}
		}
	}
	if len(keptParams) > 0 {
		out["parameters"] = keptParams
	}
	if bodyParam != nil {
		out["requestBody"] = buildRequestBodyFromBodyParam(bodyParam, consumes)
	} else if len(formDataParams) > 0 {
		out["requestBody"] = buildRequestBodyFromFormData(formDataParams)
	}

	if raw, ok := op["responses"].(map[string]any); ok {
		out["responses"] = convertSwagger2ResponsesMap(raw, produces)
	}

	// Copy the other operation fields unchanged (summary, description,
	// tags, deprecated, operationId, externalDocs, security, servers,
	// extensions).
	for k, v := range op {
		switch k {
		case "parameters", "responses", "consumes", "produces":
			continue
		}
		out[k] = v
	}
	return out
}

// convertSwagger2Response transforms a stand-alone response object from
// OAS 2 (top-level `schema`, `headers` with inline types) into OAS 3
// (`content[media].schema`, `headers.<name>.schema`).
func convertSwagger2Response(r map[string]any, produces []string) map[string]any {
	out := map[string]any{}
	for k, v := range r {
		if k == "schema" || k == "examples" || k == "headers" {
			continue
		}
		out[k] = v
	}
	if _, ok := out["description"]; !ok {
		// OAS 3 requires description; fall back to empty string to keep
		// the doc valid under strict validators.
		out["description"] = ""
	}
	if schema, ok := r["schema"]; ok {
		if len(produces) == 0 {
			produces = []string{"application/json"}
		}
		content := map[string]any{}
		for _, media := range produces {
			entry := map[string]any{"schema": convertSwagger2Schema(schema)}
			if examples, ok := r["examples"].(map[string]any); ok {
				if ex, ok := examples[media]; ok {
					entry["example"] = ex
				}
			}
			content[media] = entry
		}
		out["content"] = content
	}
	if headers, ok := r["headers"].(map[string]any); ok && len(headers) > 0 {
		converted := map[string]any{}
		for name, h := range headers {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			converted[name] = convertSwagger2Header(hm)
		}
		out["headers"] = converted
	}
	return out
}

// convertSwagger2ResponsesMap walks responses:{"200":{}, default:{}} and
// converts each entry.
func convertSwagger2ResponsesMap(resps map[string]any, produces []string) map[string]any {
	out := make(map[string]any, len(resps))
	for code, r := range resps {
		rm, ok := r.(map[string]any)
		if !ok {
			out[code] = r
			continue
		}
		out[code] = convertSwagger2Response(rm, produces)
	}
	return out
}

// convertSwagger2Parameter reshapes a non-body parameter into OAS 3
// form (loose type/format/enum lifted under `schema`). Body parameters
// are handled separately because they become a requestBody.
func convertSwagger2Parameter(p map[string]any) map[string]any {
	in, _ := p["in"].(string)
	if in == "body" {
		return nil
	}
	out := map[string]any{}
	schemaKeys := map[string]struct{}{
		"type":             {},
		"format":           {},
		"enum":             {},
		"minimum":          {},
		"maximum":          {},
		"exclusiveMinimum": {},
		"exclusiveMaximum": {},
		"minLength":        {},
		"maxLength":        {},
		"pattern":          {},
		"minItems":         {},
		"maxItems":         {},
		"uniqueItems":      {},
		"multipleOf":       {},
		"default":          {},
		"items":            {},
	}
	schema := map[string]any{}
	for k, v := range p {
		if _, inSchema := schemaKeys[k]; inSchema {
			if k == "items" {
				if im, ok := v.(map[string]any); ok {
					schema["items"] = convertSwagger2Schema(im)
				} else {
					schema["items"] = v
				}
				continue
			}
			schema[k] = v
			continue
		}
		if k == "collectionFormat" {
			// Map OAS 2 collectionFormat to OAS 3 style/explode.
			switch v {
			case "csv":
				out["style"] = "simple"
				out["explode"] = false
			case "ssv":
				out["style"] = "spaceDelimited"
			case "pipes":
				out["style"] = "pipeDelimited"
			case "multi":
				out["explode"] = true
			}
			continue
		}
		out[k] = v
	}
	if len(schema) > 0 {
		out["schema"] = schema
	}
	return out
}

// convertSwagger2ParameterList converts an array of parameters, skipping
// body/formData entries (they are rolled up into requestBody elsewhere).
func convertSwagger2ParameterList(list []any) []any {
	out := make([]any, 0, len(list))
	for _, p := range list {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if converted := convertSwagger2Parameter(pm); converted != nil {
			out = append(out, converted)
		}
	}
	return out
}

// convertSwagger2Schema handles the schema-level differences between
// OAS 2 and OAS 3: `type: file` becomes `type: string, format: binary`,
// and nested property schemas recurse through the same logic.
func convertSwagger2Schema(s any) any {
	sm, ok := s.(map[string]any)
	if !ok {
		return s
	}
	out := make(map[string]any, len(sm))
	for k, v := range sm {
		switch k {
		case "type":
			if sv, ok := v.(string); ok && sv == "file" {
				out["type"] = "string"
				out["format"] = "binary"
				continue
			}
			out[k] = v
		case "items":
			out[k] = convertSwagger2Schema(v)
		case "properties":
			if pm, ok := v.(map[string]any); ok {
				converted := make(map[string]any, len(pm))
				for name, prop := range pm {
					converted[name] = convertSwagger2Schema(prop)
				}
				out[k] = converted
			} else {
				out[k] = v
			}
		case "additionalProperties":
			out[k] = convertSwagger2Schema(v)
		case "allOf", "anyOf", "oneOf":
			if arr, ok := v.([]any); ok {
				converted := make([]any, len(arr))
				for i, item := range arr {
					converted[i] = convertSwagger2Schema(item)
				}
				out[k] = converted
			} else {
				out[k] = v
			}
		case "x-nullable":
			out["nullable"] = v
		default:
			out[k] = v
		}
	}
	return out
}

// convertSwagger2Header reshapes an OAS 2 header object (loose type
// fields) into OAS 3 form (type/format nested under schema).
func convertSwagger2Header(h map[string]any) map[string]any {
	// Headers share the parameter transform with `in` forced to a noop.
	copy := make(map[string]any, len(h))
	for k, v := range h {
		copy[k] = v
	}
	copy["in"] = "header"
	converted := convertSwagger2Parameter(copy)
	if converted == nil {
		return h
	}
	delete(converted, "in")
	delete(converted, "name")
	return converted
}

// convertSwagger2SecurityScheme maps OAS 2 securityDefinitions to OAS 3
// securitySchemes. Basic auth and the oauth2 flow variants are the
// interesting cases; apiKey is one-to-one.
func convertSwagger2SecurityScheme(ss map[string]any) map[string]any {
	typ, _ := ss["type"].(string)
	switch typ {
	case "basic":
		return map[string]any{"type": "http", "scheme": "basic"}
	case "oauth2":
		flow, _ := ss["flow"].(string)
		inner := map[string]any{}
		if v, ok := ss["authorizationUrl"]; ok {
			inner["authorizationUrl"] = v
		}
		if v, ok := ss["tokenUrl"]; ok {
			inner["tokenUrl"] = v
		}
		if v, ok := ss["scopes"]; ok {
			inner["scopes"] = v
		}
		flows := map[string]any{}
		switch flow {
		case "implicit":
			flows["implicit"] = inner
		case "password":
			flows["password"] = inner
		case "application":
			flows["clientCredentials"] = inner
		case "accessCode":
			flows["authorizationCode"] = inner
		default:
			// Preserve as-is under a custom key so information is not
			// silently dropped.
			flows[flow] = inner
		}
		out := map[string]any{"type": "oauth2", "flows": flows}
		if v, ok := ss["description"]; ok {
			out["description"] = v
		}
		return out
	}
	// apiKey / openIdConnect / http pass through mostly unchanged.
	return ss
}

// buildRequestBodyFromBodyParam converts an OAS 2 `in: body` parameter
// into an OAS 3 requestBody object.
func buildRequestBodyFromBodyParam(body map[string]any, consumes []string) map[string]any {
	if len(consumes) == 0 {
		consumes = []string{"application/json"}
	}
	rb := map[string]any{}
	if desc, ok := body["description"]; ok {
		rb["description"] = desc
	}
	if req, ok := body["required"]; ok {
		rb["required"] = req
	}
	content := map[string]any{}
	for _, media := range consumes {
		if schema, ok := body["schema"]; ok {
			content[media] = map[string]any{"schema": convertSwagger2Schema(schema)}
		} else {
			content[media] = map[string]any{}
		}
	}
	rb["content"] = content
	return rb
}

// buildRequestBodyFromFormData groups formData parameters into a single
// requestBody with a schema whose properties reflect each parameter.
func buildRequestBodyFromFormData(params []map[string]any) map[string]any {
	properties := map[string]any{}
	var required []string
	hasFile := false
	for _, p := range params {
		name, _ := p["name"].(string)
		if name == "" {
			continue
		}
		prop := map[string]any{}
		if t, ok := p["type"].(string); ok {
			if t == "file" {
				prop["type"] = "string"
				prop["format"] = "binary"
				hasFile = true
			} else {
				prop["type"] = t
			}
		}
		if f, ok := p["format"]; ok {
			prop["format"] = f
		}
		if desc, ok := p["description"]; ok {
			prop["description"] = desc
		}
		if def, ok := p["default"]; ok {
			prop["default"] = def
		}
		if enum, ok := p["enum"]; ok {
			prop["enum"] = enum
		}
		properties[name] = prop
		if req, ok := p["required"].(bool); ok && req {
			required = append(required, name)
		}
	}
	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = anySlice(required)
	}
	media := "application/x-www-form-urlencoded"
	if hasFile {
		media = "multipart/form-data"
	}
	return map[string]any{
		"content": map[string]any{
			media: map[string]any{"schema": schema},
		},
	}
}

// rewriteSwagger2Refs walks the converted map and rewrites every remaining
// Swagger 2 style $ref into an OpenAPI 3 one:
//
//	#/definitions/<N>          -> #/components/schemas/<N>
//	#/parameters/<N>           -> #/components/parameters/<N>
//	#/responses/<N>            -> #/components/responses/<N>
//	#/securityDefinitions/<N>  -> #/components/securitySchemes/<N>
//
// We operate in-place on maps/slices decoded from YAML/JSON.
func rewriteSwagger2Refs(node any) {
	switch n := node.(type) {
	case map[string]any:
		if ref, ok := n["$ref"].(string); ok {
			n["$ref"] = rewriteSwagger2Ref(ref)
		}
		for _, v := range n {
			rewriteSwagger2Refs(v)
		}
	case []any:
		for _, v := range n {
			rewriteSwagger2Refs(v)
		}
	}
}

func rewriteSwagger2Ref(ref string) string {
	switch {
	case strings.HasPrefix(ref, "#/definitions/"):
		return "#/components/schemas/" + strings.TrimPrefix(ref, "#/definitions/")
	case strings.HasPrefix(ref, "#/parameters/"):
		return "#/components/parameters/" + strings.TrimPrefix(ref, "#/parameters/")
	case strings.HasPrefix(ref, "#/responses/"):
		return "#/components/responses/" + strings.TrimPrefix(ref, "#/responses/")
	case strings.HasPrefix(ref, "#/securityDefinitions/"):
		return "#/components/securitySchemes/" + strings.TrimPrefix(ref, "#/securityDefinitions/")
	}
	return ref
}

// stringSlice extracts a []string from an `any` that came from decoded
// YAML/JSON. Missing / wrong-typed values return nil so callers can
// cleanly fall back to defaults.
func stringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// anySlice converts a []string to []any so it can be assigned into a
// decoded YAML/JSON tree.
func anySlice(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func countOperations(spec map[string]interface{}) int {
	paths, _ := spec["paths"].(map[string]interface{})
	total := 0
	methods := map[string]struct{}{
		"get": {}, "put": {}, "post": {}, "delete": {},
		"options": {}, "head": {}, "patch": {}, "trace": {},
	}
	for _, v := range paths {
		item, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		for k := range item {
			if _, ok := methods[strings.ToLower(k)]; ok {
				total++
			}
		}
	}
	return total
}

func countSchemas(spec map[string]interface{}) int {
	comp, _ := spec["components"].(map[string]interface{})
	schemas, _ := comp["schemas"].(map[string]interface{})
	return len(schemas)
}
