package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveOIDCClaims(t *testing.T) {
	tests := []struct {
		name       string
		raw        map[string]any
		mapping    OIDCMapping
		wantEmail  string
		wantName   string
		wantGroups []string
	}{
		{
			name:       "standard claims, default mapping",
			raw:        map[string]any{"email": "a@x.com", "name": "Ada", "groups": []any{"eng", "admins"}},
			wantEmail:  "a@x.com",
			wantName:   "Ada",
			wantGroups: []string{"eng", "admins"},
		},
		{
			name:       "Auth0 namespaced groups claim",
			raw:        map[string]any{"email": "a@x.com", "https://flint.dev/groups": []any{"eng"}},
			mapping:    OIDCMapping{GroupsClaim: "https://flint.dev/groups"},
			wantEmail:  "a@x.com",
			wantGroups: []string{"eng"},
		},
		{
			name:      "Google Workspace — no groups claim, no crash",
			raw:       map[string]any{"email": "a@x.com", "name": "Ada"},
			wantEmail: "a@x.com",
			wantName:  "Ada",
		},
		{
			name:       "Azure group GUIDs kept intact",
			raw:        map[string]any{"email": "a@x.com", "groups": []any{"11111111-2222-3333-4444-555555555555"}},
			wantEmail:  "a@x.com",
			wantGroups: []string{"11111111-2222-3333-4444-555555555555"},
		},
		{
			name:       "single-string groups becomes one element",
			raw:        map[string]any{"groups": "eng"},
			wantGroups: []string{"eng"},
		},
		{
			name:      "custom email + name claims",
			raw:       map[string]any{"mail": "a@x.com", "displayName": "Ada Lovelace"},
			mapping:   OIDCMapping{EmailClaim: "mail", NameClaim: "displayName"},
			wantEmail: "a@x.com",
			wantName:  "Ada Lovelace",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			email, name, groups := resolveOIDCClaims(tt.raw, tt.mapping)
			assert.Equal(t, tt.wantEmail, email)
			assert.Equal(t, tt.wantName, name)
			assert.Equal(t, tt.wantGroups, groups)
		})
	}
}

func TestResolveSAMLAttributes(t *testing.T) {
	tests := []struct {
		name       string
		attrs      map[string]any
		mapping    SAMLMapping
		wantEmail  string
		wantName   string
		wantGroups []string
	}{
		{
			name: "friendly names, default mapping",
			attrs: map[string]any{
				"email":  "a@x.com",
				"name":   "Ada",
				"groups": []string{"eng", "admins"},
			},
			wantEmail:  "a@x.com",
			wantName:   "Ada",
			wantGroups: []string{"eng", "admins"},
		},
		{
			name: "Azure WS-* email + Entra groups URI (previously dropped)",
			attrs: map[string]any{
				"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress": "a@x.com",
				"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name":         "Ada",
				"http://schemas.microsoft.com/ws/2008/06/identity/claims/groups":     []string{"g1", "g2"},
			},
			wantEmail:  "a@x.com",
			wantName:   "Ada",
			wantGroups: []string{"g1", "g2"},
		},
		{
			name:       "AD memberOf",
			attrs:      map[string]any{"email": "a@x.com", "memberOf": []string{"CN=Eng,OU=Groups"}},
			wantEmail:  "a@x.com",
			wantGroups: []string{"CN=Eng,OU=Groups"},
		},
		{
			name:       "custom attribute names override defaults",
			attrs:      map[string]any{"urn:mail": "a@x.com", "urn:team": []string{"eng"}},
			mapping:    SAMLMapping{EmailAttrs: []string{"urn:mail"}, GroupsAttrs: []string{"urn:team"}},
			wantEmail:  "a@x.com",
			wantGroups: []string{"eng"},
		},
		{
			name:      "no groups present, no crash",
			attrs:     map[string]any{"email": "a@x.com"},
			wantEmail: "a@x.com",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			email, name, groups := resolveSAMLAttributes(tt.attrs, tt.mapping)
			assert.Equal(t, tt.wantEmail, email)
			assert.Equal(t, tt.wantName, name)
			assert.Equal(t, tt.wantGroups, groups)
		})
	}
}

func TestOIDCGroupOverage(t *testing.T) {
	t.Run("Azure distributed-claims overage is detected with the Graph endpoint", func(t *testing.T) {
		raw := map[string]any{
			"_claim_names": map[string]any{"groups": "src1"},
			"_claim_sources": map[string]any{
				"src1": map[string]any{"endpoint": "https://graph.microsoft.com/v1.0/users/abc/getMemberObjects"},
			},
		}
		over, url := oidcGroupOverage(raw)
		assert.True(t, over)
		assert.Equal(t, "https://graph.microsoft.com/v1.0/users/abc/getMemberObjects", url)
	})

	t.Run("overage marker without a source still flags overage", func(t *testing.T) {
		over, url := oidcGroupOverage(map[string]any{"_claim_names": map[string]any{"groups": "src1"}})
		assert.True(t, over)
		assert.Empty(t, url)
	})

	t.Run("normal claims with a real groups list are not overage", func(t *testing.T) {
		over, _ := oidcGroupOverage(map[string]any{"groups": []any{"eng", "admins"}})
		assert.False(t, over)
	})

	t.Run("resolve returns no groups but does not panic on overage", func(t *testing.T) {
		raw := map[string]any{
			"email":        "a@b.com",
			"_claim_names": map[string]any{"groups": "src1"},
		}
		_, _, groups := resolveOIDCClaims(raw, OIDCMapping{})
		assert.Empty(t, groups)
	})
}

func TestSAMLGroupOverage(t *testing.T) {
	over, url := samlGroupOverage(map[string]any{
		"http://schemas.microsoft.com/claims/groups.link": "https://graph.microsoft.com/v1.0/users/abc/getMemberObjects",
	})
	assert.True(t, over)
	assert.Equal(t, "https://graph.microsoft.com/v1.0/users/abc/getMemberObjects", url)

	over2, _ := samlGroupOverage(map[string]any{"groups": "eng"})
	assert.False(t, over2)
}

func TestCoerceStringSlice(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want []string
	}{
		{"nil", nil, nil},
		{"empty string", "", nil},
		{"single string", "eng", []string{"eng"}},
		{"string slice", []string{"a", "b"}, []string{"a", "b"}},
		{"string slice drops empties", []string{"a", "", "b"}, []string{"a", "b"}},
		{"any slice", []any{"a", "b"}, []string{"a", "b"}},
		{"any slice mixed drops non-strings", []any{"a", 42, "b"}, []string{"a", "b"}},
		{"comma string is NOT split", "a,b", []string{"a,b"}},
		{"non-string scalar", 42, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, coerceStringSlice(tt.in))
		})
	}
}
