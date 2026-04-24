// Copyright (c) 2020-2025. Sailpoint Technologies, Inc. All rights reserved.

package goextract

import (
	"go/token"
	"testing"
)

func TestCamelToKebab(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"GovernanceGroup", "governance-group"},
		{"IdentityCreated", "identity-created"},
		{"ABC", "a-b-c"},
		{"simple", "simple"},
		{"CamelCase", "camel-case"},
		{"TopicName", "topic-name"},
		{"", ""},
	}

	for _, tt := range tests {
		result := camelToKebab(tt.input)
		if result != tt.expected {
			t.Errorf("camelToKebab(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestKebabToCamel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"governance-group", "governanceGroup"},
		{"identity-created", "identityCreated"},
		{"simple", "simple"},
		{"multi-word-name", "multiWordName"},
		{"", ""},
	}

	for _, tt := range tests {
		result := kebabToCamel(tt.input)
		if result != tt.expected {
			t.Errorf("kebabToCamel(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestSnakeToCamel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"GOVERNANCE_GROUP", "governanceGroup"},
		{"IDENTITY_CREATED", "identityCreated"},
		{"SIMPLE", "simple"},
		{"MULTI_WORD_NAME", "multiWordName"},
		{"", ""},
	}

	for _, tt := range tests {
		result := snakeToCamel(tt.input)
		if result != tt.expected {
			t.Errorf("snakeToCamel(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestFormatTopicName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"governance-group", "Governance Group"},
		{"identity-created", "Identity Created"},
		{"simple", "Simple"},
		{"multi-word-topic-name", "Multi Word Topic Name"},
	}

	for _, tt := range tests {
		result := formatTopicName(tt.input)
		if result != tt.expected {
			t.Errorf("formatTopicName(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestFormatEventType(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"GOVERNANCE_GROUP", "Governance Group"},
		{"IDENTITY_CREATED", "Identity Created"},
		{"SIMPLE", "Simple"},
		{"MULTI_WORD_EVENT", "Multi Word Event"},
	}

	for _, tt := range tests {
		result := formatEventType(tt.input)
		if result != tt.expected {
			t.Errorf("formatEventType(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestEventAnalyzer_ConvertTopicIdentifier(t *testing.T) {
	ea := NewEventAnalyzer(token.NewFileSet())

	tests := []struct {
		input    string
		expected string
	}{
		{"TopicGovernanceGroup", "governance-group"},
		{"TOPIC_IDENTITY_CREATED", "identity-created"},
		{"IdnWorkgroup", "workgroup"},
		// After removing IDN_, "ACCOUNT" has no underscores, so camelToKebab is used
		// All uppercase letters are treated as word boundaries
		{"IDN_ACCOUNT_V1", "account-v1"},
		{"SimpleTopic", "simple-topic"},
	}

	for _, tt := range tests {
		result := ea.convertTopicIdentifier(tt.input)
		if result != tt.expected {
			t.Errorf("convertTopicIdentifier(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestEventAnalyzer_GenerateWebhookName(t *testing.T) {
	ea := NewEventAnalyzer(token.NewFileSet())

	tests := []struct {
		topic     string
		eventType string
		prefix    string
		expected  string
	}{
		{"governance-group", "CREATED", "", "governanceGroupCreated"},
		{"identity", "CHANGED", "on", "onIdentityChanged"},
		{"", "ACCOUNT_UPDATED", "", "accountUpdated"},
		{"workgroup", "", "", "workgroup"},
		{"", "", "publish", "publish"},
	}

	for _, tt := range tests {
		result := ea.generateWebhookName(tt.topic, tt.eventType, tt.prefix)
		if result != tt.expected {
			t.Errorf("generateWebhookName(%q, %q, %q) = %q, want %q",
				tt.topic, tt.eventType, tt.prefix, result, tt.expected)
		}
	}
}

func TestEventAnalyzer_GenerateWebhookSummary(t *testing.T) {
	ea := NewEventAnalyzer(token.NewFileSet())

	tests := []struct {
		webhook  *WebhookInfo
		dir      string
		expected string
	}{
		{
			webhook:  &WebhookInfo{Topic: "governance-group", EventType: "CREATED"},
			dir:      "publish",
			expected: "Governance Group Created Event",
		},
		{
			webhook:  &WebhookInfo{Topic: "identity", EventType: "CHANGED"},
			dir:      "consume",
			expected: "Identity Changed Handler",
		},
		{
			webhook:  &WebhookInfo{Topic: "", EventType: ""},
			dir:      "publish",
			expected: "Event",
		},
	}

	for _, tt := range tests {
		result := ea.generateWebhookSummary(tt.webhook, tt.dir)
		if result != tt.expected {
			t.Errorf("generateWebhookSummary(%v, %q) = %q, want %q",
				tt.webhook, tt.dir, result, tt.expected)
		}
	}
}

func TestParseWebhookAnnotations(t *testing.T) {
	tests := []struct {
		name     string
		comments string
		expected *WebhookInfo
	}{
		{
			name: "full annotation",
			comments: `
// @openapi:webhook governanceGroupCreated
// @openapi:webhook:topic governance-group-v1
// @openapi:webhook:eventType CREATED
// @openapi:webhook:description Fired when a governance group is created
// @openapi:webhook:payload WorkgroupEntityEventDTO
`,
			expected: &WebhookInfo{
				Name:        "governanceGroupCreated",
				Topic:       "governance-group-v1",
				EventType:   "CREATED",
				Description: "Fired when a governance group is created",
				PayloadType: "WorkgroupEntityEventDTO",
				Direction:   "publish",
			},
		},
		{
			name: "minimal annotation",
			comments: `
// @openapi:webhook identityChanged
`,
			expected: &WebhookInfo{
				Name:      "identityChanged",
				Direction: "publish",
			},
		},
		{
			name:     "no annotation",
			comments: "// Just a regular comment",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseWebhookAnnotations(tt.comments)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %+v", result)
				}
				return
			}

			if result == nil {
				t.Errorf("expected %+v, got nil", tt.expected)
				return
			}

			if result.Name != tt.expected.Name {
				t.Errorf("Name: got %q, want %q", result.Name, tt.expected.Name)
			}
			if result.Topic != tt.expected.Topic {
				t.Errorf("Topic: got %q, want %q", result.Topic, tt.expected.Topic)
			}
			if result.EventType != tt.expected.EventType {
				t.Errorf("EventType: got %q, want %q", result.EventType, tt.expected.EventType)
			}
			if result.Description != tt.expected.Description {
				t.Errorf("Description: got %q, want %q", result.Description, tt.expected.Description)
			}
			if result.PayloadType != tt.expected.PayloadType {
				t.Errorf("PayloadType: got %q, want %q", result.PayloadType, tt.expected.PayloadType)
			}
		})
	}
}

func TestIsContextType(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"context.Context", true},
		{"Context", true},
		{"mypackage.Context", true},
		{"*context.Context", true}, // Contains "Context" so it's a context type
		{"MyStruct", false},
		{"string", false},
		{"MyDTO", false},
	}

	for _, tt := range tests {
		result := isContextType(tt.input)
		if result != tt.expected {
			t.Errorf("isContextType(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestNewEventAnalyzer(t *testing.T) {
	fset := token.NewFileSet()
	ea := NewEventAnalyzer(fset)

	if ea == nil {
		t.Fatal("NewEventAnalyzer returned nil")
	}

	if ea.webhooks == nil {
		t.Error("webhooks map not initialized")
	}

	if ea.topicConstants == nil {
		t.Error("topicConstants map not initialized")
	}

	if ea.fset != fset {
		t.Error("fset not set correctly")
	}
}

func TestEventAnalyzer_GetWebhooks(t *testing.T) {
	ea := NewEventAnalyzer(token.NewFileSet())

	// Add a test webhook
	webhook := &WebhookInfo{
		Name:      "testWebhook",
		Topic:     "test-topic",
		EventType: "TEST_EVENT",
		Direction: "publish",
	}
	ea.webhooks["testWebhook"] = webhook

	webhooks := ea.GetWebhooks()

	if len(webhooks) != 1 {
		t.Errorf("expected 1 webhook, got %d", len(webhooks))
	}

	if webhooks["testWebhook"] != webhook {
		t.Error("webhook not found or incorrect")
	}
}

func TestEventAnalyzer_IsEventRouterMethod(t *testing.T) {
	ea := NewEventAnalyzer(token.NewFileSet())

	tests := []struct {
		name     string
		expected bool
	}{
		{"OnTopic", true},
		{"OnTopicAndEventType", true},
		{"On", true},
		{"HandleEvent", true},
		{"Subscribe", true},
		{"Publish", false},
		{"RandomMethod", false},
	}

	for _, tt := range tests {
		result := ea.isEventRouterMethod(tt.name)
		if result != tt.expected {
			t.Errorf("isEventRouterMethod(%q) = %v, want %v", tt.name, result, tt.expected)
		}
	}
}

func TestEventAnalyzer_IsEventPublishMethod(t *testing.T) {
	ea := NewEventAnalyzer(token.NewFileSet())

	tests := []struct {
		name     string
		expected bool
	}{
		{"Publish", true},
		{"PublishEvent", true},
		{"Send", true},
		{"Emit", true},
		{"Fire", true},
		{"publish", true}, // Case insensitive check
		{"OnTopic", false},
		{"HandleEvent", false},
	}

	for _, tt := range tests {
		result := ea.isEventPublishMethod(tt.name)
		if result != tt.expected {
			t.Errorf("isEventPublishMethod(%q) = %v, want %v", tt.name, result, tt.expected)
		}
	}
}

