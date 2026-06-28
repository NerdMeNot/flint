package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// classifyAuthError turns raw provider errors into the short, field-level
// summaries the sign-in diagnostics log shows. These cases mirror the failure
// modes admins actually hit when wiring up an IdP.
func TestClassifyAuthError(t *testing.T) {
	cases := []struct {
		name   string
		detail string
		reason string
		want   string
	}{
		{"no provider", "", "no_provider", "No SSO provider configured"},
		{"saml audience", "expected audience https://sp.example.com but response had urn:other", "saml:validation_failed", "Audience (SP Entity ID) mismatch"},
		{"oidc nonce", "nonce in ID token does not match expected nonce", "oidc:exchange_failed", "Nonce mismatch (possible replay)"},
		{"saml destination", "`Destination` does not match ACS url", "saml:validation_failed", "ACS URL / destination mismatch"},
		{"saml recipient", "wrong Recipient on the subject confirmation", "saml:validation_failed", "ACS URL / destination mismatch"},
		{"signature", "could not validate signature on the response", "saml:validation_failed", "Signature verification failed"},
		{"oidc verifying", "failed verifying ID token signature", "oidc:exchange_failed", "Signature verification failed"},
		{"expired assertion", "assertion NotOnOrAfter has passed", "saml:validation_failed", "Assertion expired or clock skew"},
		{"clock skew", "response IssueInstant clock skew too large", "saml:validation_failed", "Assertion expired or clock skew"},
		{"no email", "no email claim present in the ID token", "oidc:exchange_failed", "No email returned by the IdP"},
		{"issuer", "ID token issued by a different issuer", "oidc:exchange_failed", "Issuer mismatch"},
		{"unknown detail", "some unmapped provider error", "oidc:exchange_failed", "Authentication failed"},
		{"empty", "", "oidc:exchange_failed", "Authentication failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, classifyAuthError(tc.detail, tc.reason))
		})
	}
}
