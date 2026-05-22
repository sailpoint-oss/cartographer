package index

import (
	"strings"
	"unicode"
)

// parseJacksonNamingStrategy extracts a normalized strategy key from
// @JsonNaming annotation arguments, e.g.:
//
//	@JsonNaming(SnakeCaseStrategy.class)
//	@JsonNaming(PropertyNamingStrategies.SnakeCaseStrategy.class)
func parseJacksonNamingStrategy(argList string) string {
	argList = strings.TrimSpace(argList)
	argList = strings.TrimPrefix(argList, "(")
	argList = strings.TrimSuffix(argList, ")")
	argList = strings.TrimSpace(argList)
	upper := strings.ToUpper(argList)

	switch {
	case strings.Contains(upper, "SNAKE_CASE") || strings.Contains(upper, "SNAKECASE"):
		return "snake_case"
	case strings.Contains(upper, "UPPER_CAMEL") || strings.Contains(upper, "UPPERCAMEL"):
		return "upper_camel"
	case strings.Contains(upper, "KEBAB") || strings.Contains(upper, "KEBAB_CASE"):
		return "kebab_case"
	case strings.Contains(upper, "LOWER_CAMEL") || strings.Contains(upper, "LOWERCAMEL"):
		return "lower_camel"
	case strings.Contains(upper, "LOWERCASE") || strings.Contains(upper, "LOWER_CASE"):
		return "lower_case"
	default:
		return ""
	}
}

func hasExplicitJsonPropertyWireName(field FieldDecl) bool {
	jp, ok := field.Annotations["JsonProperty"]
	if !ok || jp == "" {
		return false
	}
	return extractAnnotationStringValue(jp) != ""
}

// applyJacksonNaming converts a Java field name to the JSON wire name
// using the class-level Jackson naming strategy.
func applyJacksonNaming(fieldName, strategy string) string {
	switch strategy {
	case "snake_case":
		return toSnakeCase(fieldName)
	case "upper_camel":
		return toUpperCamel(fieldName)
	case "lower_case":
		return strings.ToLower(fieldName)
	case "kebab_case":
		return toKebabCase(fieldName)
	case "lower_camel":
		return toLowerCamel(fieldName)
	default:
		return fieldName
	}
}

func applyClassJacksonNaming(fields []FieldDecl, strategy string) {
	if strategy == "" {
		return
	}
	for i := range fields {
		if hasExplicitJsonPropertyWireName(fields[i]) {
			continue
		}
		fields[i].JSONName = applyJacksonNaming(fields[i].Name, strategy)
	}
}

func toSnakeCase(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func toKebabCase(s string) string {
	return strings.ReplaceAll(toSnakeCase(s), "_", "-")
}

func toUpperCamel(s string) string {
	if s == "" {
		return s
	}
	parts := splitIdentifierParts(s)
	for i := range parts {
		if parts[i] == "" {
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + strings.ToLower(parts[i][1:])
	}
	return strings.Join(parts, "")
}

func toLowerCamel(s string) string {
	if s == "" {
		return s
	}
	parts := splitIdentifierParts(s)
	for i := range parts {
		if parts[i] == "" {
			continue
		}
		if i == 0 {
			parts[i] = strings.ToLower(parts[i][:1]) + parts[i][1:]
		} else {
			parts[i] = strings.ToUpper(parts[i][:1]) + strings.ToLower(parts[i][1:])
		}
	}
	return strings.Join(parts, "")
}

func splitIdentifierParts(s string) []string {
	if strings.Contains(s, "_") {
		return strings.Split(s, "_")
	}
	var parts []string
	start := 0
	for i := 1; i < len(s); i++ {
		if unicode.IsUpper(rune(s[i])) && unicode.IsLower(rune(s[i-1])) {
			parts = append(parts, s[start:i])
			start = i
		}
	}
	parts = append(parts, s[start:])
	return parts
}
