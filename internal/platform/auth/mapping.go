package auth

// Claim/attribute mapping. IdPs are standards-compliant on the wire but wildly
// inconsistent about *which* claim or attribute carries the user's email, name,
// and group membership. Rather than branch per provider, the OIDC and SAML
// providers extract a raw bag of claims/attributes and resolve the unified
// Claims fields through the configurable mappings below. Presets (in the web UI)
// merely pre-fill these names; the engine stays generic.

// OIDCMapping names the ID-token / userinfo claims that carry each unified field.
// Empty fields fall back to the OIDC-conventional names.
type OIDCMapping struct {
	EmailClaim  string
	NameClaim   string
	GroupsClaim string
}

// withDefaults returns a copy with conventional claim names filled in.
func (m OIDCMapping) withDefaults() OIDCMapping {
	if m.EmailClaim == "" {
		m.EmailClaim = "email"
	}
	if m.NameClaim == "" {
		m.NameClaim = "name"
	}
	if m.GroupsClaim == "" {
		m.GroupsClaim = "groups"
	}
	return m
}

// SAMLMapping lists, in priority order, the SAML attribute names that may carry
// each unified field. Empty lists fall back to the well-known defaults, which
// cover the standard WS-* URIs plus the common friendly names and — importantly
// — the Microsoft Entra (Azure AD) groups URI that the old hardcoded switch
// missed.
type SAMLMapping struct {
	EmailAttrs  []string
	NameAttrs   []string
	GroupsAttrs []string
}

var (
	defaultSAMLEmailAttrs = []string{
		"email",
		"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress",
	}
	defaultSAMLNameAttrs = []string{
		"name",
		"displayName",
		"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name",
	}
	defaultSAMLGroupsAttrs = []string{
		"groups",
		"memberOf",
		"http://schemas.xmlsoap.org/claims/Group",
		// Microsoft Entra ID / Azure AD emits groups under this URI.
		"http://schemas.microsoft.com/ws/2008/06/identity/claims/groups",
	}
)

// withDefaults returns a copy with the well-known attribute names filled in.
func (m SAMLMapping) withDefaults() SAMLMapping {
	if len(m.EmailAttrs) == 0 {
		m.EmailAttrs = defaultSAMLEmailAttrs
	}
	if len(m.NameAttrs) == 0 {
		m.NameAttrs = defaultSAMLNameAttrs
	}
	if len(m.GroupsAttrs) == 0 {
		m.GroupsAttrs = defaultSAMLGroupsAttrs
	}
	return m
}

// resolveOIDCClaims pulls the unified fields out of a raw claims map using the
// configured (or default) claim names.
func resolveOIDCClaims(raw map[string]any, m OIDCMapping) (email, name string, groups []string) {
	m = m.withDefaults()
	return coerceString(raw[m.EmailClaim]),
		coerceString(raw[m.NameClaim]),
		coerceStringSlice(raw[m.GroupsClaim])
}

// resolveSAMLAttributes pulls the unified fields out of a SAML attribute bag,
// trying each candidate attribute name in priority order and taking the first
// that is present.
func resolveSAMLAttributes(attrs map[string]any, m SAMLMapping) (email, name string, groups []string) {
	m = m.withDefaults()
	email = firstString(attrs, m.EmailAttrs)
	name = firstString(attrs, m.NameAttrs)
	for _, attr := range m.GroupsAttrs {
		if v, ok := attrs[attr]; ok {
			if g := coerceStringSlice(v); len(g) > 0 {
				groups = g
				break
			}
		}
	}
	return email, name, groups
}

// firstString returns the first non-empty string found across the candidate keys.
func firstString(attrs map[string]any, keys []string) string {
	for _, k := range keys {
		if v, ok := attrs[k]; ok {
			if s := coerceString(v); s != "" {
				return s
			}
		}
	}
	return ""
}

// coerceString extracts a single string from an arbitrary claim/attribute value.
// Multi-valued inputs yield their first element. Non-string scalars yield "".
func coerceString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []string:
		if len(t) > 0 {
			return t[0]
		}
	case []any:
		if len(t) > 0 {
			return coerceString(t[0])
		}
	}
	return ""
}

// coerceStringSlice normalizes an arbitrary claim/attribute value into a string
// slice. It deliberately does NOT split on commas — group names can legitimately
// contain commas, so a single string becomes a single-element slice (this also
// keeps Azure group GUIDs intact). Empty elements are dropped.
func coerceStringSlice(v any) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case []string:
		return nonEmpty(t)
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s := coerceString(e); s != "" {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func nonEmpty(in []string) []string {
	out := in[:0:0]
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
