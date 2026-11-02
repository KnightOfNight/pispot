package authz

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// cacheTTL is how long a successful auth result is cached. During this
// window the pispot-authd socket is not consulted for repeat requests
// with the same credentials.
const cacheTTL = 5 * time.Minute

// cachedEntry holds the role and expiry for one cached credential pair.
type cachedEntry struct {
	role    string
	expires time.Time
}

// authCache is a concurrency-safe in-memory cache of auth results.
// Keys are username + ":" + hex(SHA256(password)), so plaintext
// passwords are never stored.
type authCache struct {
	mu    sync.Mutex
	items map[string]cachedEntry
}

// activeCache is the cache used by the running middleware, so account
// changes can drop cached logins.
var activeCache atomic.Pointer[authCache]

// InvalidateUser drops cached logins for username, so a deleted or demoted
// account, or an old password, stops working immediately instead of when the
// cache entry expires.
func InvalidateUser(username string) {
	if c := activeCache.Load(); c != nil {
		c.invalidate(username)
	}
}

func newAuthCache() *authCache {
	return &authCache{items: make(map[string]cachedEntry)}
}

func cacheKey(username, password string) string {
	h := sha256.Sum256([]byte(password))
	return username + ":" + hex.EncodeToString(h[:])
}

func (c *authCache) invalidate(username string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	prefix := username + ":"
	for key := range c.items {
		if strings.HasPrefix(key, prefix) {
			delete(c.items, key)
		}
	}
}

// get returns the cached role and true if a non-expired entry exists.
func (c *authCache) get(username, password string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[cacheKey(username, password)]
	if !ok || time.Now().After(e.expires) {
		return "", false
	}
	return e.role, true
}

// set stores a role for the given credentials with the standard TTL.
func (c *authCache) set(username, password, role string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[cacheKey(username, password)] = cachedEntry{
		role:    role,
		expires: time.Now().Add(cacheTTL),
	}
}
