package web

import (
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// parseCIDRList turns a comma-separated list of IPs/CIDRs into networks. Bare
// IPs become /32 (or /128). Invalid entries are skipped and logged by the caller
// via the returned skipped list.
func parseCIDRList(raw string) (nets []*net.IPNet, skipped []string) {
	for _, part := range strings.Split(raw, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if !strings.Contains(p, "/") {
			if ip := net.ParseIP(p); ip != nil {
				bits := 32
				if ip.To4() == nil {
					bits = 128
				}
				p = p + "/" + strconv.Itoa(bits)
			}
		}
		_, n, err := net.ParseCIDR(p)
		if err != nil {
			skipped = append(skipped, part)
			continue
		}
		nets = append(nets, n)
	}
	return nets, skipped
}

// ipInList reports whether ip is inside any of nets.
func ipInList(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// proxyAuth authenticates a request from a trusted reverse proxy (Authelia,
// Authentik, …) via an identity header. The header is honored ONLY when the
// request's direct peer is in the allowlist, so a client that is not the
// configured proxy cannot forge an identity. On success the mapped local user is
// placed in the context; on any miss the request falls through to classic
// session auth, so normal login keeps working.
func proxyAuth(header string, allow []*net.IPNet, autoProvision bool, defaultRole domain.Role, users *store.UserRepo, audit *store.AuthEventRepo, ssoLog *tokenUseLog) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := net.ParseIP(clientIP(r))
			if ip == nil || !ipInList(ip, allow) {
				next.ServeHTTP(w, r) // untrusted source: ignore the header entirely
				return
			}
			username := strings.TrimSpace(r.Header.Get(header))
			if username == "" {
				next.ServeHTTP(w, r)
				return
			}
			u, err := users.GetByUsername(username)
			if errors.Is(err, store.ErrNotFound) {
				if !autoProvision {
					next.ServeHTTP(w, r) // unknown identity, provisioning off → fall back to login
					return
				}
				u, err = provisionProxyUser(users, username, defaultRole)
			}
			if err != nil {
				serverError(w, r, err)
				return
			}
			if ssoLog.shouldLog(u.Username, time.Now().UTC()) {
				recordAuth(audit, r, domain.AuthSSOLogin, u.Username, u.ID, "proxy")
			}
			next.ServeHTTP(w, r.WithContext(withUser(r.Context(), u)))
		})
	}
}

// provisionProxyUser creates a local account for a proxy-asserted identity with
// an unusable password (the user authenticates only via the proxy). It tolerates
// a concurrent create by re-fetching on a duplicate.
func provisionProxyUser(users *store.UserRepo, username string, role domain.Role) (domain.User, error) {
	u := domain.User{Username: username, Role: role}
	if err := u.Validate(); err != nil {
		return domain.User{}, err
	}
	// A random, discarded password → password login is effectively impossible.
	rnd, err := newSessionToken()
	if err != nil {
		return domain.User{}, err
	}
	hash, err := hashPassword(rnd)
	if err != nil {
		return domain.User{}, err
	}
	now := nowRFC3339()
	u.PasswordHash, u.CreatedAt, u.UpdatedAt = hash, now, now
	id, err := users.Create(u)
	if err != nil {
		// Likely a concurrent provision of the same identity: re-fetch.
		if existing, gerr := users.GetByUsername(username); gerr == nil {
			return existing, nil
		}
		return domain.User{}, err
	}
	u.ID = id
	return u, nil
}
