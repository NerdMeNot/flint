package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockGraph is a faithful stand-in for Microsoft Graph + the Entra token
// endpoint, so the GraphClient's token flow, overage fetch, paging, and
// GUID→name resolution are all exercised end-to-end without a real tenant.
type mockGraph struct {
	srv       *httptest.Server
	groups    map[string]string // object id -> display name
	member    map[string][]string
	pageBy    int // if >0, paginate getMemberObjects into chunks of this size
	getByIdsN int // count of directoryObjects/getByIds calls (to assert caching)
}

func newMockGraph(t *testing.T) *mockGraph {
	t.Helper()
	m := &mockGraph{groups: map[string]string{}, member: map[string][]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/tenant-1/oauth2/v2.0/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"graph-token","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/v1.0/directoryObjects/getByIds", func(w http.ResponseWriter, r *http.Request) {
		m.getByIdsN++
		var req struct {
			IDs []string `json:"ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var out struct {
			Value []map[string]string `json:"value"`
		}
		for _, id := range req.IDs {
			if name, ok := m.groups[id]; ok {
				out.Value = append(out.Value, map[string]string{"id": id, "displayName": name})
			}
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// getMemberObjects: /v1.0/users/{id}/getMemberObjects
		if strings.HasSuffix(r.URL.Path, "/getMemberObjects") {
			uid := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1.0/users/"), "/getMemberObjects")
			all := m.member[uid]
			skip := 0
			if tok := r.URL.Query().Get("$skiptoken"); tok != "" {
				skip, _ = strconv.Atoi(tok)
			}
			resp := map[string]any{}
			if m.pageBy > 0 && skip+m.pageBy < len(all) {
				resp["value"] = all[skip : skip+m.pageBy]
				resp["@odata.nextLink"] = m.srv.URL + r.URL.Path + "?$skiptoken=" + strconv.Itoa(skip+m.pageBy)
			} else {
				end := len(all)
				start := skip
				if start > end {
					start = end
				}
				resp["value"] = all[start:end]
			}
			writeJSON(w, resp)
			return
		}
		http.NotFound(w, r)
	})
	m.srv = httptest.NewServer(mux)
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mockGraph) client() *GraphClient {
	return NewGraphClient(GraphConfig{
		TenantID: "tenant-1", ClientID: "app", ClientSecret: "secret",
		BaseURL: m.srv.URL, LoginURL: m.srv.URL,
	})
}

func TestGraphClient_ResolveGroups(t *testing.T) {
	ctx := context.Background()

	t.Run("resolves GUID group claims to display names", func(t *testing.T) {
		m := newMockGraph(t)
		m.groups["11111111-1111-1111-1111-111111111111"] = "Engineering"
		m.groups["22222222-2222-2222-2222-222222222222"] = "Admins"
		g := m.client()

		out, err := g.ResolveGroups(ctx, "user-1",
			[]string{"11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"}, false)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"Engineering", "Admins"}, out)
	})

	t.Run("keeps already-named groups and resolves only the GUIDs", func(t *testing.T) {
		m := newMockGraph(t)
		m.groups["11111111-1111-1111-1111-111111111111"] = "Engineering"
		g := m.client()

		out, err := g.ResolveGroups(ctx, "user-1",
			[]string{"platform-team", "11111111-1111-1111-1111-111111111111"}, false)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"platform-team", "Engineering"}, out)
	})

	t.Run("on overage, fetches the full membership from Graph then resolves names", func(t *testing.T) {
		m := newMockGraph(t)
		m.member["user-9"] = []string{
			"11111111-1111-1111-1111-111111111111",
			"22222222-2222-2222-2222-222222222222",
		}
		m.groups["11111111-1111-1111-1111-111111111111"] = "Engineering"
		m.groups["22222222-2222-2222-2222-222222222222"] = "Admins"
		g := m.client()

		// claimGroups is empty because Azure dropped it (overage).
		out, err := g.ResolveGroups(ctx, "user-9", nil, true)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"Engineering", "Admins"}, out)
	})

	t.Run("overage fetch pages through getMemberObjects", func(t *testing.T) {
		m := newMockGraph(t)
		m.pageBy = 1
		m.member["user-p"] = []string{
			"11111111-1111-1111-1111-111111111111",
			"22222222-2222-2222-2222-222222222222",
			"33333333-3333-3333-3333-333333333333",
		}
		m.groups["11111111-1111-1111-1111-111111111111"] = "A"
		m.groups["22222222-2222-2222-2222-222222222222"] = "B"
		m.groups["33333333-3333-3333-3333-333333333333"] = "C"
		g := m.client()

		out, err := g.ResolveGroups(ctx, "user-p", nil, true)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"A", "B", "C"}, out)
	})

	t.Run("unresolvable GUID is kept as-is rather than dropped", func(t *testing.T) {
		m := newMockGraph(t)
		g := m.client()
		out, err := g.ResolveGroups(ctx, "user-1", []string{"99999999-9999-9999-9999-999999999999"}, false)
		require.NoError(t, err)
		assert.Equal(t, []string{"99999999-9999-9999-9999-999999999999"}, out)
	})

	t.Run("overage with no object id errors rather than silently dropping access", func(t *testing.T) {
		m := newMockGraph(t)
		g := m.client()
		_, err := g.ResolveGroups(ctx, "", nil, true)
		assert.Error(t, err)
	})
}

func TestGraphClient_NameCache(t *testing.T) {
	m := newMockGraph(t)
	m.groups["11111111-1111-1111-1111-111111111111"] = "Engineering"
	g := m.client()
	ctx := context.Background()
	guids := []string{"11111111-1111-1111-1111-111111111111"}

	r1, err := g.ResolveGroups(ctx, "u", guids, false)
	require.NoError(t, err)
	r2, err := g.ResolveGroups(ctx, "u", guids, false)
	require.NoError(t, err)

	assert.Equal(t, []string{"Engineering"}, r1)
	assert.Equal(t, r1, r2)
	assert.Equal(t, 1, m.getByIdsN, "second resolution should be served from cache, not Graph")
}

func TestNewGraphClient_NilWhenUnconfigured(t *testing.T) {
	assert.Nil(t, NewGraphClient(GraphConfig{}))
	assert.Nil(t, NewGraphClient(GraphConfig{TenantID: "t"}))
	assert.NotNil(t, NewGraphClient(GraphConfig{TenantID: "t", ClientID: "c", ClientSecret: "s"}))
}

func TestIsGUID(t *testing.T) {
	assert.True(t, isGUID("11111111-1111-1111-1111-111111111111"))
	assert.True(t, isGUID("ABCDEF01-2345-6789-abcd-ef0123456789"))
	assert.False(t, isGUID("Engineering"))
	assert.False(t, isGUID("platform-team"))
	assert.False(t, isGUID("11111111111111111111111111111111111"))
	assert.False(t, isGUID("zzzzzzzz-1111-1111-1111-111111111111"))
}
