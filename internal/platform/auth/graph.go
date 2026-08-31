package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// Microsoft Graph integration for Azure AD / Entra ID. Azure delivers group
// membership as opaque GUIDs (not names), and omits it entirely past the
// overage threshold (~150 SAML / 200 OIDC). When an admin configures app
// credentials, Flint calls Graph to (a) fetch the full membership when the
// claim overflowed, and (b) resolve GUIDs to display names so group→role
// mapping can be authored by name. All Azure-specific behaviour is isolated
// here; the generic OIDC/SAML engines stay provider-agnostic.

// GraphConfig holds the Azure app-registration credentials used for the
// client-credentials flow. Requires the GroupMember.Read.All (or
// Directory.Read.All) application permission with admin consent.
type GraphConfig struct {
	TenantID     string
	ClientID     string
	ClientSecret string
	// BaseURL and LoginURL default to the public Microsoft cloud; overridable
	// for sovereign clouds (US Gov, China) and for testing.
	BaseURL  string
	LoginURL string
}

func (c GraphConfig) configured() bool {
	return c.TenantID != "" && c.ClientID != "" && c.ClientSecret != ""
}

// GraphClient resolves Azure group GUIDs to names and fetches overage memberships.
type GraphClient struct {
	ts      oauth2.TokenSource
	http    *http.Client
	baseURL string

	// nameCache memoizes group GUID→display-name so high-volume logins don't
	// re-hit Graph (and its throttling limits) for the same groups every time.
	mu        sync.RWMutex
	nameCache map[string]nameEntry
	cacheTTL  time.Duration
}

type nameEntry struct {
	name string
	exp  time.Time
}

// NewGraphClient builds a Graph client backed by an auto-refreshing
// client-credentials token source. Returns nil when credentials are absent, so
// callers can treat "no Graph" as simply nil.
func NewGraphClient(cfg GraphConfig) *GraphClient {
	if !cfg.configured() {
		return nil
	}
	login := strings.TrimRight(orDefault(cfg.LoginURL, "https://login.microsoftonline.com"), "/")
	base := strings.TrimRight(orDefault(cfg.BaseURL, "https://graph.microsoft.com"), "/")
	cc := &clientcredentials.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		TokenURL:     fmt.Sprintf("%s/%s/oauth2/v2.0/token", login, cfg.TenantID),
		Scopes:       []string{base + "/.default"},
	}
	return &GraphClient{
		ts:        cc.TokenSource(context.Background()),
		http:      &http.Client{Timeout: 10 * time.Second},
		baseURL:   base,
		nameCache: map[string]nameEntry{},
		cacheTTL:  time.Hour,
	}
}

// ResolveGroups returns the user's group membership as display names. It is the
// single entry point the providers call after claim resolution:
//   - on overage (claimGroups empty, IdP signalled it), it fetches the full set
//     of group object IDs from Graph;
//   - otherwise it uses claimGroups as-is;
//   - any GUID-shaped entries are then resolved to display names, leaving
//     already-named groups untouched.
//
// userObjectID is the Azure directory object ID (the `oid` claim / SAML
// objectidentifier attribute), required for the overage fetch.
func (g *GraphClient) ResolveGroups(ctx context.Context, userObjectID string, claimGroups []string, overage bool) ([]string, error) {
	ids := claimGroups
	if overage {
		if userObjectID == "" {
			return claimGroups, fmt.Errorf("group overage but no user object id to query Graph")
		}
		fetched, err := g.memberGroups(ctx, userObjectID)
		if err != nil {
			return claimGroups, err
		}
		ids = fetched
	}

	// Partition into GUIDs (resolve to names) and already-named groups (keep).
	var guids []string
	out := make([]string, 0, len(ids))
	for _, v := range ids {
		if isGUID(v) {
			guids = append(guids, v)
		} else {
			out = append(out, v)
		}
	}
	if len(guids) == 0 {
		return out, nil
	}
	names, err := g.groupDisplayNames(ctx, guids)
	if err != nil {
		return ids, err // fall back to raw IDs on resolution failure
	}
	for _, id := range guids {
		if n := names[id]; n != "" {
			out = append(out, n)
		} else {
			out = append(out, id) // unresolved (e.g. not a group) — keep the GUID
		}
	}
	return out, nil
}

// memberGroups fetches the transitive group membership (object IDs) for a user,
// the authoritative source when the login claim overflowed. Uses
// getMemberObjects so it works regardless of group count, paging through results.
func (g *GraphClient) memberGroups(ctx context.Context, userObjectID string) ([]string, error) {
	url := fmt.Sprintf("%s/v1.0/users/%s/getMemberObjects", g.baseURL, userObjectID)
	var ids []string
	for url != "" {
		var page struct {
			Value    []string `json:"value"`
			NextLink string   `json:"@odata.nextLink"`
		}
		// getMemberObjects is a POST; the (rare) paged nextLink is followed with GET.
		method, body := http.MethodPost, `{"securityEnabledOnly":false}`
		if strings.Contains(url, "$skiptoken") {
			method, body = http.MethodGet, ""
		}
		if err := g.do(ctx, method, url, body, &page); err != nil {
			return nil, err
		}
		ids = append(ids, page.Value...)
		url = page.NextLink
	}
	return ids, nil
}

// groupDisplayNames resolves group object IDs to display names via the
// directoryObjects/getByIds batch endpoint (chunked at the Graph limit of 1000).
func (g *GraphClient) groupDisplayNames(ctx context.Context, ids []string) (map[string]string, error) {
	out := make(map[string]string, len(ids))

	// Serve cache hits; collect the misses to fetch.
	var miss []string
	now := time.Now()
	g.mu.RLock()
	for _, id := range ids {
		if e, ok := g.nameCache[id]; ok && e.exp.After(now) {
			out[id] = e.name
		} else {
			miss = append(miss, id)
		}
	}
	g.mu.RUnlock()
	if len(miss) == 0 {
		return out, nil
	}

	for _, chunk := range chunkStrings(miss, 1000) {
		payload, _ := json.Marshal(map[string]any{"ids": chunk, "types": []string{"group"}})
		var resp struct {
			Value []struct {
				ID          string `json:"id"`
				DisplayName string `json:"displayName"`
			} `json:"value"`
		}
		url := g.baseURL + "/v1.0/directoryObjects/getByIds"
		if err := g.do(ctx, http.MethodPost, url, string(payload), &resp); err != nil {
			return nil, err
		}
		exp := time.Now().Add(g.cacheTTL)
		g.mu.Lock()
		for _, v := range resp.Value {
			out[v.ID] = v.DisplayName
			g.nameCache[v.ID] = nameEntry{name: v.DisplayName, exp: exp}
		}
		g.mu.Unlock()
	}
	return out, nil
}

// do performs an authenticated Graph request and decodes the JSON body into out.
func (g *GraphClient) do(ctx context.Context, method, url, body string, out any) error {
	var req *http.Request
	var err error
	if body != "" {
		req, err = http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
	} else {
		req, err = http.NewRequestWithContext(ctx, method, url, nil)
	}
	if err != nil {
		return err
	}
	tok, err := g.ts.Token()
	if err != nil {
		return fmt.Errorf("graph token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("graph %s %s: status %d", method, url, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// isGUID reports whether s is a canonical 8-4-4-4-12 hex UUID (an Azure object
// ID), as opposed to a human-readable group name.
func isGUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !isHexDigit(r) {
				return false
			}
		}
	}
	return true
}

// anyGUID reports whether any entry is a GUID — i.e. whether name resolution
// would change anything, used to gate the Graph round trip.
func anyGUID(groups []string) bool {
	for _, g := range groups {
		if isGUID(g) {
			return true
		}
	}
	return false
}

func chunkStrings(s []string, size int) [][]string {
	var out [][]string
	for i := 0; i < len(s); i += size {
		end := i + size
		if end > len(s) {
			end = len(s)
		}
		out = append(out, s[i:end])
	}
	return out
}

// isHexDigit reports whether r is a hexadecimal digit in either case.
//
// Named rather than inlined as a negated disjunction: staticcheck is right that
// `!(a || b || c)` wants De Morgan's law, but the mechanical rewrite —
// `r < '0' || r > '9' && …` over three ranges — is correct and unreadable. A
// name says what the check is for.
func isHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}
