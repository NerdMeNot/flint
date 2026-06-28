package server

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
)

// SCIM 2.0 (RFC 7643/7644) wire types and helpers. SCIM has its own JSON
// envelope and error format, distinct from Flint's API error envelope, so these
// responses are written directly rather than through the api* helpers.

const (
	scimUserSchema  = "urn:ietf:params:scim:schemas:core:2.0:User"
	scimGroupSchema = "urn:ietf:params:scim:schemas:core:2.0:Group"
	scimListSchema  = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	scimErrorSchema = "urn:ietf:params:scim:api:messages:2.0:Error"
	scimPatchSchema = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	scimContentType = "application/scim+json"
)

type scimMeta struct {
	ResourceType string `json:"resourceType"`
	Created      string `json:"created,omitempty"`
	LastModified string `json:"lastModified,omitempty"`
	Location     string `json:"location"`
}

type scimEmail struct {
	Value   string `json:"value"`
	Type    string `json:"type,omitempty"`
	Primary bool   `json:"primary,omitempty"`
}

type scimName struct {
	Formatted  string `json:"formatted,omitempty"`
	GivenName  string `json:"givenName,omitempty"`
	FamilyName string `json:"familyName,omitempty"`
}

type scimUser struct {
	Schemas     []string    `json:"schemas"`
	ID          string      `json:"id"`
	ExternalID  string      `json:"externalId,omitempty"`
	UserName    string      `json:"userName"`
	Name        *scimName   `json:"name,omitempty"`
	DisplayName string      `json:"displayName,omitempty"`
	Emails      []scimEmail `json:"emails,omitempty"`
	Active      bool        `json:"active"`
	Meta        scimMeta    `json:"meta"`
}

type scimMember struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
}

type scimGroup struct {
	Schemas     []string     `json:"schemas"`
	ID          string       `json:"id"`
	DisplayName string       `json:"displayName"`
	Members     []scimMember `json:"members"`
	Meta        scimMeta     `json:"meta"`
}

type scimListResponse struct {
	Schemas      []string `json:"schemas"`
	TotalResults int      `json:"totalResults"`
	StartIndex   int      `json:"startIndex"`
	ItemsPerPage int      `json:"itemsPerPage"`
	Resources    []any    `json:"Resources"`
}

// scimUserBody is the request body accepted on POST/PUT /Users.
type scimUserBody struct {
	ExternalID string      `json:"externalId"`
	UserName   string      `json:"userName"`
	Name       *scimName   `json:"name"`
	Emails     []scimEmail `json:"emails"`
	Active     *bool       `json:"active"`
}

// scimGroupBody is the request body accepted on POST/PUT /Groups.
type scimGroupBody struct {
	ExternalID  string       `json:"externalId"`
	DisplayName string       `json:"displayName"`
	Members     []scimMember `json:"members"`
}

// scimPatchOp is a single PATCH operation (RFC 7644 §3.5.2).
type scimPatchOp struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
}

type scimPatchBody struct {
	Schemas    []string      `json:"schemas"`
	Operations []scimPatchOp `json:"Operations"`
}

// writeSCIM marshals body as application/scim+json with the given status.
func writeSCIM(c *app.RequestContext, status int, body any) {
	b, err := json.Marshal(body)
	if err != nil {
		c.SetStatusCode(500)
		return
	}
	c.Response.Header.SetContentType(scimContentType)
	c.SetStatusCode(status)
	_, _ = c.Write(b)
}

// writeSCIMError emits an RFC 7644 error response.
func writeSCIMError(c *app.RequestContext, status int, detail string) {
	writeSCIM(c, status, map[string]any{
		"schemas": []string{scimErrorSchema},
		"detail":  detail,
		"status":  strconv.Itoa(status),
	})
}

// scimUserResource builds a SCIM User from Flint user fields.
func scimUserResource(baseURL, id, externalID, email, name string, active bool, created time.Time) scimUser {
	u := scimUser{
		Schemas:     []string{scimUserSchema},
		ID:          id,
		ExternalID:  externalID,
		UserName:    email,
		DisplayName: name,
		Emails:      []scimEmail{{Value: email, Type: "work", Primary: true}},
		Active:      active,
		Meta:        scimMeta{ResourceType: "User", Location: baseURL + "/scim/v2/Users/" + id},
	}
	if name != "" {
		u.Name = &scimName{Formatted: name}
	}
	if !created.IsZero() {
		u.Meta.Created = created.UTC().Format(time.RFC3339)
		u.Meta.LastModified = u.Meta.Created
	}
	return u
}

// scimGroupResource builds a SCIM Group from a Flint team + members.
func scimGroupResource(baseURL, id, displayName string, members []scimMember) scimGroup {
	if members == nil {
		members = []scimMember{}
	}
	return scimGroup{
		Schemas:     []string{scimGroupSchema},
		ID:          id,
		DisplayName: displayName,
		Members:     members,
		Meta:        scimMeta{ResourceType: "Group", Location: baseURL + "/scim/v2/Groups/" + id},
	}
}
