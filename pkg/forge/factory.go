package forge

import "fmt"

// NewProvider creates a ForgeProvider from a type string and credentials.
func NewProvider(forgeType, token string, appAuth *GitHubAppAuth) (ForgeProvider, error) {
	switch forgeType {
	case "github":
		return NewGitHub(token, appAuth), nil
	case "gitlab":
		return NewGitLab(token)
	case "bitbucket":
		return NewBitbucketWithToken(token)
	default:
		return nil, fmt.Errorf("forge: unsupported provider type %q", forgeType)
	}
}
