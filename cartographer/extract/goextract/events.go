// Copyright (c) 2020-2025. Sailpoint Technologies, Inc. All rights reserved.

package goextract

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"regexp"
	"strings"
	"unicode"
)

// EventAnalyzer analyzes event-related code patterns to extract webhook information.
// It detects both event consumption (OnTopic) and event publishing patterns.
type EventAnalyzer struct {
	// Collected webhooks
	webhooks map[string]*WebhookInfo

	// Track topic constant values for resolution
	topicConstants map[string]string

	// File set for position information
	fset *token.FileSet
}

// NewEventAnalyzer creates a new EventAnalyzer.
func NewEventAnalyzer(fset *token.FileSet) *EventAnalyzer {
	return &EventAnalyzer{
		webhooks:       make(map[string]*WebhookInfo),
		topicConstants: make(map[string]string),
		fset:           fset,
	}
}

// GetWebhooks returns all detected webhooks.
func (ea *EventAnalyzer) GetWebhooks() map[string]*WebhookInfo {
	return ea.webhooks
}

// AnalyzeFile analyzes an AST file for event patterns.
func (ea *EventAnalyzer) AnalyzeFile(file *ast.File, info *types.Info, filePath string) {
	// First pass: collect topic constants
	ea.collectTopicConstants(file, info)

	// Second pass: find event registrations and publications
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			ea.analyzeCallExpr(node, file, info, filePath)
		}
		return true
	})
}

// collectTopicConstants scans for topic constant declarations.
// Patterns:
//   - const TopicName TopicDescriptor = "topic-name"
//   - var TopicGovernanceGroup = idn.Topic("governance-group-v1")
func (ea *EventAnalyzer) collectTopicConstants(file *ast.File, info *types.Info) {
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}

		if genDecl.Tok != token.CONST && genDecl.Tok != token.VAR {
			continue
		}

		for _, spec := range genDecl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}

			for i, name := range valueSpec.Names {
				if i < len(valueSpec.Values) {
					if topicValue := ea.extractTopicValue(valueSpec.Values[i], info); topicValue != "" {
						ea.topicConstants[name.Name] = topicValue
					}
				}
			}
		}
	}
}

// extractTopicValue extracts a topic string from an expression.
func (ea *EventAnalyzer) extractTopicValue(expr ast.Expr, info *types.Info) string {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind == token.STRING && len(e.Value) >= 2 {
			return e.Value[1 : len(e.Value)-1]
		}
	case *ast.CallExpr:
		// Pattern: idn.Topic("topic-name") or TopicDescriptor("topic-name")
		if len(e.Args) > 0 {
			if lit, ok := e.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING && len(lit.Value) >= 2 {
				return lit.Value[1 : len(lit.Value)-1]
			}
		}
	}
	return ""
}

// analyzeCallExpr analyzes a call expression for event patterns.
func (ea *EventAnalyzer) analyzeCallExpr(call *ast.CallExpr, file *ast.File, info *types.Info, filePath string) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}

	methodName := sel.Sel.Name

	// Check for event router patterns (consumption)
	if ea.isEventRouterMethod(methodName) {
		ea.analyzeEventRouterCall(call, sel, info, filePath)
		return
	}

	// Check for event publishing patterns
	if ea.isEventPublishMethod(methodName) {
		ea.analyzeEventPublishCall(call, sel, info, filePath)
		return
	}
}

// isEventRouterMethod checks if a method name indicates event routing/consumption.
func (ea *EventAnalyzer) isEventRouterMethod(name string) bool {
	consumptionMethods := []string{
		"OnTopic",
		"OnTopicAndEventType",
		"On",
		"HandleEvent",
		"Subscribe",
	}
	for _, m := range consumptionMethods {
		if name == m {
			return true
		}
	}
	return false
}

// isEventPublishMethod checks if a method name indicates event publishing.
func (ea *EventAnalyzer) isEventPublishMethod(name string) bool {
	publishMethods := []string{
		"Publish",
		"PublishEvent",
		"Send",
		"Emit",
		"Fire",
	}
	for _, m := range publishMethods {
		if name == m || strings.EqualFold(name, m) {
			return true
		}
	}
	return false
}

// analyzeEventRouterCall analyzes event.Router.OnTopic() and similar calls.
func (ea *EventAnalyzer) analyzeEventRouterCall(call *ast.CallExpr, sel *ast.SelectorExpr, info *types.Info, filePath string) {
	if len(call.Args) < 1 {
		return
	}

	webhook := &WebhookInfo{
		Direction: "consume",
		File:      filePath,
		Tags:      []string{"Webhooks"},
	}

	if ea.fset != nil {
		webhook.Line = ea.fset.Position(call.Pos()).Line
	}

	methodName := sel.Sel.Name

	switch methodName {
	case "OnTopic":
		// Pattern: router.OnTopic(topicDescriptor, handler)
		if len(call.Args) >= 1 {
			webhook.Topic = ea.extractTopicFromArg(call.Args[0], info)
		}
		if len(call.Args) >= 2 {
			webhook.PayloadType = ea.extractHandlerPayloadType(call.Args[1], info)
		}

	case "OnTopicAndEventType":
		// Pattern: router.OnTopicAndEventType(topicDescriptor, eventType, handler)
		if len(call.Args) >= 1 {
			webhook.Topic = ea.extractTopicFromArg(call.Args[0], info)
		}
		if len(call.Args) >= 2 {
			webhook.EventType = ea.extractStringArg(call.Args[1], info)
		}
		if len(call.Args) >= 3 {
			webhook.PayloadType = ea.extractHandlerPayloadType(call.Args[2], info)
		}

	case "On", "HandleEvent", "Subscribe":
		// Generic patterns - extract what we can
		for _, arg := range call.Args {
			if topic := ea.extractTopicFromArg(arg, info); topic != "" && webhook.Topic == "" {
				webhook.Topic = topic
			}
		}
	}

	// Generate webhook name from topic and event type
	webhook.Name = ea.generateWebhookName(webhook.Topic, webhook.EventType, "on")

	if webhook.Name != "" && (webhook.Topic != "" || webhook.EventType != "") {
		webhook.Summary = ea.generateWebhookSummary(webhook, "consume")
		webhook.Description = ea.generateWebhookDescription(webhook, "consume")
		ea.webhooks[webhook.Name] = webhook
	}
}

// analyzeEventPublishCall analyzes EventService.Publish() and similar calls.
func (ea *EventAnalyzer) analyzeEventPublishCall(call *ast.CallExpr, sel *ast.SelectorExpr, info *types.Info, filePath string) {
	if len(call.Args) < 1 {
		return
	}

	webhook := &WebhookInfo{
		Direction: "publish",
		File:      filePath,
		Tags:      []string{"Webhooks"},
	}

	if ea.fset != nil {
		webhook.Line = ea.fset.Position(call.Pos()).Line
	}

	// Pattern: eventService.Publish(topic, event)
	// Pattern: publisher.Publish(topic, eventType, payload)
	for i, arg := range call.Args {
		// First arg is often the topic
		if i == 0 {
			webhook.Topic = ea.extractTopicFromArg(arg, info)
			continue
		}

		// Try to extract event type (usually a string constant)
		if webhook.EventType == "" {
			if eventType := ea.extractStringArg(arg, info); eventType != "" {
				webhook.EventType = eventType
				continue
			}
		}

		// Try to extract payload type from the argument
		if webhook.PayloadType == "" {
			webhook.PayloadType = ea.extractPayloadTypeFromArg(arg, info)
		}
	}

	// Generate webhook name from topic and event type
	webhook.Name = ea.generateWebhookName(webhook.Topic, webhook.EventType, "")

	if webhook.Name != "" && (webhook.Topic != "" || webhook.EventType != "") {
		webhook.Summary = ea.generateWebhookSummary(webhook, "publish")
		webhook.Description = ea.generateWebhookDescription(webhook, "publish")
		ea.webhooks[webhook.Name] = webhook
	}
}

// extractTopicFromArg extracts a topic name from a function argument.
func (ea *EventAnalyzer) extractTopicFromArg(arg ast.Expr, info *types.Info) string {
	switch e := arg.(type) {
	case *ast.BasicLit:
		// Direct string literal
		if e.Kind == token.STRING && len(e.Value) >= 2 {
			return e.Value[1 : len(e.Value)-1]
		}

	case *ast.Ident:
		// Local variable or constant
		if topic, ok := ea.topicConstants[e.Name]; ok {
			return topic
		}
		// Try to resolve constant value
		if info != nil && info.Uses != nil {
			if obj := info.Uses[e]; obj != nil {
				if constObj, ok := obj.(*types.Const); ok {
					if constObj.Val() != nil && constObj.Val().Kind() == constant.String {
						return constant.StringVal(constObj.Val())
					}
				}
			}
		}
		// Return the identifier name as fallback (might be a known topic constant)
		return ea.convertTopicIdentifier(e.Name)

	case *ast.SelectorExpr:
		// Package.Constant pattern: idn.TopicGovernanceGroup
		if topic := ea.resolveSelectorTopic(e, info); topic != "" {
			return topic
		}
		// Return selector name as fallback
		return ea.convertTopicIdentifier(e.Sel.Name)
	}

	return ""
}

// resolveSelectorTopic resolves a selector expression to a topic string.
func (ea *EventAnalyzer) resolveSelectorTopic(sel *ast.SelectorExpr, info *types.Info) string {
	if info != nil && info.Uses != nil {
		if obj := info.Uses[sel.Sel]; obj != nil {
			if constObj, ok := obj.(*types.Const); ok {
				if constObj.Val() != nil && constObj.Val().Kind() == constant.String {
					return constant.StringVal(constObj.Val())
				}
			}
		}
	}
	return ""
}

// convertTopicIdentifier converts a Go identifier to a topic name.
// e.g., "TopicGovernanceGroup" -> "governance-group"
// e.g., "TOPIC_IDENTITY_CREATED" -> "identity-created"
func (ea *EventAnalyzer) convertTopicIdentifier(name string) string {
	// Remove common prefixes
	name = strings.TrimPrefix(name, "Topic")
	name = strings.TrimPrefix(name, "TOPIC_")
	name = strings.TrimPrefix(name, "Idn")
	name = strings.TrimPrefix(name, "IDN_")

	if name == "" {
		return ""
	}

	// Check if it's SCREAMING_SNAKE_CASE
	if strings.Contains(name, "_") {
		// Convert SCREAMING_SNAKE_CASE to kebab-case
		parts := strings.Split(name, "_")
		for i, part := range parts {
			parts[i] = strings.ToLower(part)
		}
		return strings.Join(parts, "-")
	}

	// Convert CamelCase to kebab-case
	return camelToKebab(name)
}

// extractStringArg extracts a string value from an argument.
func (ea *EventAnalyzer) extractStringArg(arg ast.Expr, info *types.Info) string {
	switch e := arg.(type) {
	case *ast.BasicLit:
		if e.Kind == token.STRING && len(e.Value) >= 2 {
			return e.Value[1 : len(e.Value)-1]
		}

	case *ast.Ident:
		// Try to resolve constant value
		if info != nil && info.Uses != nil {
			if obj := info.Uses[e]; obj != nil {
				if constObj, ok := obj.(*types.Const); ok {
					if constObj.Val() != nil && constObj.Val().Kind() == constant.String {
						return constant.StringVal(constObj.Val())
					}
				}
			}
		}

	case *ast.SelectorExpr:
		// Package.Constant pattern
		if info != nil && info.Uses != nil {
			if obj := info.Uses[e.Sel]; obj != nil {
				if constObj, ok := obj.(*types.Const); ok {
					if constObj.Val() != nil && constObj.Val().Kind() == constant.String {
						return constant.StringVal(constObj.Val())
					}
				}
			}
		}
	}

	return ""
}

// extractHandlerPayloadType extracts the payload type from a handler function.
func (ea *EventAnalyzer) extractHandlerPayloadType(handler ast.Expr, info *types.Info) string {
	// Handler could be a function literal or a reference to a function
	switch h := handler.(type) {
	case *ast.FuncLit:
		// Inline handler function - look at parameters
		return ea.extractPayloadTypeFromFuncType(h.Type, info)

	case *ast.Ident:
		// Reference to a named function or variable
		if info != nil && info.Uses != nil {
			if obj := info.Uses[h]; obj != nil {
				if fn, ok := obj.(*types.Func); ok {
					if sig, ok := fn.Type().(*types.Signature); ok {
						return ea.extractPayloadTypeFromSignature(sig)
					}
				}
			}
		}

	case *ast.SelectorExpr:
		// Method reference: s.HandleEvent
		if info != nil && info.Selections != nil {
			if sel := info.Selections[h]; sel != nil {
				if fn, ok := sel.Obj().(*types.Func); ok {
					if sig, ok := fn.Type().(*types.Signature); ok {
						return ea.extractPayloadTypeFromSignature(sig)
					}
				}
			}
		}
	}

	return ""
}

// extractPayloadTypeFromFuncType extracts payload type from a function type AST node.
func (ea *EventAnalyzer) extractPayloadTypeFromFuncType(funcType *ast.FuncType, info *types.Info) string {
	if funcType.Params == nil || len(funcType.Params.List) == 0 {
		return ""
	}

	// Look for an *Event parameter or similar
	for _, param := range funcType.Params.List {
		typeName := ea.extractTypeName(param.Type, info)
		if typeName != "" && !isContextType(typeName) {
			return typeName
		}
	}

	return ""
}

// extractPayloadTypeFromSignature extracts payload type from a types.Signature.
func (ea *EventAnalyzer) extractPayloadTypeFromSignature(sig *types.Signature) string {
	params := sig.Params()
	if params == nil || params.Len() == 0 {
		return ""
	}

	// Look for the event payload parameter (skip context.Context)
	for i := 0; i < params.Len(); i++ {
		param := params.At(i)
		typeName := TypeString(param.Type())
		if typeName != "" && !isContextType(typeName) {
			return typeName
		}
	}

	return ""
}

// extractPayloadTypeFromArg extracts payload type from an argument expression.
func (ea *EventAnalyzer) extractPayloadTypeFromArg(arg ast.Expr, info *types.Info) string {
	if info == nil || info.Types == nil {
		return ""
	}

	if tv, ok := info.Types[arg]; ok {
		return TypeString(tv.Type)
	}

	return ""
}

// extractTypeName extracts a type name from an AST type expression.
func (ea *EventAnalyzer) extractTypeName(typeExpr ast.Expr, info *types.Info) string {
	switch t := typeExpr.(type) {
	case *ast.Ident:
		return t.Name

	case *ast.SelectorExpr:
		if x, ok := t.X.(*ast.Ident); ok {
			return x.Name + "." + t.Sel.Name
		}
		return t.Sel.Name

	case *ast.StarExpr:
		return "*" + ea.extractTypeName(t.X, info)

	case *ast.ArrayType:
		return "[]" + ea.extractTypeName(t.Elt, info)
	}

	// Try to get type from type info
	if info != nil && info.Types != nil {
		if tv, ok := info.Types[typeExpr]; ok {
			return TypeString(tv.Type)
		}
	}

	return ""
}

// isContextType checks if a type name is context.Context.
func isContextType(typeName string) bool {
	return typeName == "context.Context" ||
		typeName == "Context" ||
		strings.HasSuffix(typeName, ".Context")
}

// generateWebhookName generates a webhook name from topic and event type.
func (ea *EventAnalyzer) generateWebhookName(topic, eventType, prefix string) string {
	var parts []string

	if prefix != "" {
		parts = append(parts, prefix)
	}

	if topic != "" {
		// Convert topic to camelCase
		topicPart := kebabToCamel(topic)
		parts = append(parts, topicPart)
	}

	if eventType != "" {
		// Convert event type to camelCase
		eventPart := snakeToCamel(eventType)
		parts = append(parts, eventPart)
	}

	if len(parts) == 0 {
		return ""
	}

	// Join parts with first part lowercase
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			result += strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}

	return result
}

// generateWebhookSummary generates a summary for the webhook.
func (ea *EventAnalyzer) generateWebhookSummary(webhook *WebhookInfo, direction string) string {
	var parts []string

	if webhook.Topic != "" {
		parts = append(parts, formatTopicName(webhook.Topic))
	}

	if webhook.EventType != "" {
		parts = append(parts, formatEventType(webhook.EventType))
	}

	if len(parts) == 0 {
		return "Event"
	}

	summary := strings.Join(parts, " ")

	if direction == "consume" {
		return summary + " Handler"
	}
	return summary + " Event"
}

// generateWebhookDescription generates a description for the webhook.
func (ea *EventAnalyzer) generateWebhookDescription(webhook *WebhookInfo, direction string) string {
	if direction == "consume" {
		return "Triggered when a " + strings.ToLower(formatEventType(webhook.EventType)) +
			" event is received on the " + webhook.Topic + " topic."
	}
	return "Published when a " + strings.ToLower(formatEventType(webhook.EventType)) +
		" event occurs. Sent to the " + webhook.Topic + " topic."
}

// formatTopicName formats a topic name for display.
func formatTopicName(topic string) string {
	// Convert kebab-case to Title Case
	parts := strings.Split(topic, "-")
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, " ")
}

// formatEventType formats an event type for display.
func formatEventType(eventType string) string {
	// Convert SCREAMING_SNAKE_CASE to Title Case
	parts := strings.Split(eventType, "_")
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
		}
	}
	return strings.Join(parts, " ")
}

// camelToKebab converts CamelCase to kebab-case.
func camelToKebab(s string) string {
	var result strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				result.WriteRune('-')
			}
			result.WriteRune(unicode.ToLower(r))
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// kebabToCamel converts kebab-case to camelCase.
func kebabToCamel(s string) string {
	parts := strings.Split(s, "-")
	for i, part := range parts {
		if len(part) > 0 {
			if i == 0 {
				parts[i] = strings.ToLower(part)
			} else {
				parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
			}
		}
	}
	return strings.Join(parts, "")
}

// snakeToCamel converts SCREAMING_SNAKE_CASE to camelCase.
func snakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	for i, part := range parts {
		if len(part) > 0 {
			if i == 0 {
				parts[i] = strings.ToLower(part)
			} else {
				parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
			}
		}
	}
	return strings.Join(parts, "")
}

// WebhookAnnotationPattern matches @openapi:webhook annotations in comments.
var WebhookAnnotationPattern = regexp.MustCompile(`@openapi:webhook(?::(\w+))?\s+(.+)`)

// ParseWebhookAnnotations extracts webhook information from comment annotations.
// Supported annotations:
//   - @openapi:webhook name
//   - @openapi:webhook:topic topic-name
//   - @openapi:webhook:eventType EVENT_TYPE
//   - @openapi:webhook:description Description text
//   - @openapi:webhook:payload PayloadTypeName
func ParseWebhookAnnotations(comments string) *WebhookInfo {
	webhook := &WebhookInfo{
		Direction: "publish",
		Tags:      []string{"Webhooks"},
	}

	lines := strings.Split(comments, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "//")
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimSpace(line)

		matches := WebhookAnnotationPattern.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		subKey := matches[1]
		value := strings.TrimSpace(matches[2])

		switch subKey {
		case "":
			webhook.Name = value
		case "topic":
			webhook.Topic = value
		case "eventType":
			webhook.EventType = value
		case "description":
			webhook.Description = value
		case "payload":
			webhook.PayloadType = value
		case "summary":
			webhook.Summary = value
		}
	}

	if webhook.Name == "" {
		return nil
	}

	return webhook
}

