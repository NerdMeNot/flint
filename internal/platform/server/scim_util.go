package server

import (
	"encoding/json"
	"strings"
	"time"
)

func timeZero() time.Time { return time.Time{} }

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// slugify converts a display name into a url-safe team slug.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-':
			b.WriteRune(r)
			lastDash = r == '-'
		case r == ' ' || r == '_' || r == '.' || r == '@' || r == '/':
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// primaryEmail extracts the user's email from a SCIM user body, preferring
// userName and falling back to the primary (or first) email.
func primaryEmail(body scimUserBody) string {
	if body.UserName != "" {
		return body.UserName
	}
	for _, e := range body.Emails {
		if e.Primary && e.Value != "" {
			return e.Value
		}
	}
	if len(body.Emails) > 0 {
		return body.Emails[0].Value
	}
	return ""
}

func formattedName(n *scimName) string {
	if n == nil {
		return ""
	}
	if n.Formatted != "" {
		return n.Formatted
	}
	return strings.TrimSpace(n.GivenName + " " + n.FamilyName)
}

// scimFilterEq parses a simple SCIM `attr eq "value"` filter, returning the
// lowercased attribute and the unquoted value. Returns empty strings if the
// filter is absent or unsupported (compound filters aren't handled).
func scimFilterEq(filter string) (attr, value string) {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return "", ""
	}
	// Split on the first " eq " (case-insensitive).
	lower := strings.ToLower(filter)
	idx := strings.Index(lower, " eq ")
	if idx < 0 {
		return "", ""
	}
	attr = strings.ToLower(strings.TrimSpace(filter[:idx]))
	value = strings.TrimSpace(filter[idx+4:])
	value = strings.Trim(value, `"`)
	return attr, value
}

// patchActiveValue reads the boolean `active` value from a PATCH op, handling
// both `{"path":"active","value":false}` and the path-less
// `{"value":{"active":false}}` shapes that IdPs emit.
func patchActiveValue(op scimPatchOp) (bool, bool) {
	if strings.EqualFold(strings.TrimSpace(op.Path), "active") {
		var b bool
		if json.Unmarshal(op.Value, &b) == nil {
			return b, true
		}
		return false, false
	}
	if strings.TrimSpace(op.Path) == "" {
		var obj struct {
			Active *bool `json:"active"`
		}
		if json.Unmarshal(op.Value, &obj) == nil && obj.Active != nil {
			return *obj.Active, true
		}
	}
	return false, false
}

// memberIDFromPath extracts the user id from a filtered member path of the form
// `members[value eq "USER_ID"]` (the shape Okta uses to remove a single member).
func memberIDFromPath(path string) string {
	lower := strings.ToLower(path)
	if !strings.HasPrefix(lower, "members[") {
		return ""
	}
	open := strings.Index(path, "[")
	close := strings.LastIndex(path, "]")
	if open < 0 || close < 0 || close < open {
		return ""
	}
	inner := path[open+1 : close]
	idx := strings.Index(strings.ToLower(inner), "eq")
	if idx < 0 {
		return ""
	}
	return strings.Trim(strings.TrimSpace(inner[idx+2:]), `"`)
}

// parseMembers decodes a PATCH members value, which may be an array of member
// objects or a single member object.
func parseMembers(raw json.RawMessage) []scimMember {
	if len(raw) == 0 {
		return nil
	}
	var arr []scimMember
	if json.Unmarshal(raw, &arr) == nil {
		return arr
	}
	var one scimMember
	if json.Unmarshal(raw, &one) == nil && one.Value != "" {
		return []scimMember{one}
	}
	return nil
}
