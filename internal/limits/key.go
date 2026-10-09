package limits

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/beavend/beavend/internal/core"
)

// UserKey generates a Redis Cluster-compatible hash-tagged key for a user.
// The hash tag `{lim:project:env:user_hash}` ensures all limit keys for a user
// map to the same Redis Cluster hash slot.
func UserKey(projectID, environment string, id *core.Identity) string {
	projectID = strings.TrimSpace(projectID)
	environment = strings.TrimSpace(environment)
	if projectID == "" {
		projectID = "default"
	}
	if environment == "" {
		environment = "default"
	}

	if id == nil || (id.Subject == "" && id.Issuer == "") {
		return fmt.Sprintf("{lim:%s:%s:anonymous}", projectID, environment)
	}

	raw := id.Issuer + ":" + id.Subject
	h := sha256.Sum256([]byte(raw))
	userHash := hex.EncodeToString(h[:16]) // 32 hex chars

	return fmt.Sprintf("{lim:%s:%s:%s}", projectID, environment, userHash)
}

// RuleKey returns the base key for a specific limit rule under a user's hash tag.
func RuleKey(userKey string, rule core.LimitRule) string {
	windowSec := int64(rule.Window.Seconds())
	if windowSec <= 0 {
		windowSec = 1
	}
	res := strings.TrimSpace(rule.Resource)
	if res == "" {
		res = "inference"
	}
	return fmt.Sprintf("%s:%s:%d", userKey, res, windowSec)
}

// TokenWindowsKey returns the key used to track active token window durations for a user.
func TokenWindowsKey(userKey string) string {
	return fmt.Sprintf("%s:token_windows", userKey)
}

// ProjectKey generates a Redis Cluster-compatible hash-tagged key for project-level limits.
func ProjectKey(projectID, environment string) string {
	projectID = strings.TrimSpace(projectID)
	environment = strings.TrimSpace(environment)
	if projectID == "" {
		projectID = "default"
	}
	if environment == "" {
		environment = "default"
	}
	return fmt.Sprintf("{lim:%s:%s:project}", projectID, environment)
}
