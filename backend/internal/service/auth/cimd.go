package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ClientMetadata is the subset of RFC 7591 client metadata fields this
// server needs from a Client ID Metadata Document (CIMD), as defined by
// draft-ietf-oauth-client-id-metadata-document.
type ClientMetadata struct {
	ClientID                string   `json:"client_id"`
	RedirectURIs            []string `json:"redirect_uris,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
}

const (
	cimdMaxBodyBytes = 5 * 1024 // draft-ietf-oauth-client-id-metadata-document §2: "recommended maximum size to read is 5 kilobytes"
	cimdFetchTimeout = 5 * time.Second
	cimdDefaultTTL   = 10 * time.Minute
	cimdMinTTL       = 60 * time.Second
	cimdMaxTTL       = 24 * time.Hour
)

// CIMDResolver fetches and validates Client ID Metadata Documents so a
// client_id can itself be the client's registration, with no server-side
// persistence required (see draft-ietf-oauth-client-id-metadata-document).
type CIMDResolver interface {
	// IsClientIDURL reports whether clientID has the shape of a Client ID
	// Metadata Document URL (an https URL with a path, no userinfo/fragment).
	// It does not fetch anything.
	IsClientIDURL(clientID string) bool
	// Resolve fetches (or returns a cached, previously-validated copy of) the
	// client metadata document at the clientID URL. Callers MUST abort the
	// authorization request on error, per the draft's guidance.
	Resolve(ctx context.Context, clientID string) (*ClientMetadata, error)
}

type cimdCacheEntry struct {
	metadata  *ClientMetadata
	expiresAt time.Time
}

type cimdResolver struct {
	client *http.Client

	mu    sync.Mutex
	cache map[string]cimdCacheEntry
}

// NewCIMDResolver returns a CIMDResolver that fetches documents over a
// transport hardened against SSRF: it resolves DNS itself and refuses to
// connect to loopback/private/link-local/unspecified addresses, and it never
// follows HTTP redirects (both required by the draft).
func NewCIMDResolver() CIMDResolver {
	dialer := &net.Dialer{Timeout: cimdFetchTimeout}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			if len(ips) == 0 {
				return nil, fmt.Errorf("cimd: %q did not resolve", host)
			}
			// Reject the host outright if ANY of its addresses is special-use,
			// rather than picking a routable one. A name that resolves to both
			// a public and an internal address is the shape of a rebinding
			// attack, and a legitimate client metadata host has no reason to.
			for _, ip := range ips {
				if isSpecialUseIP(ip) {
					return nil, fmt.Errorf("cimd: %q resolves to the special-use address %s", host, ip)
				}
			}
			// Dial the address that was actually validated, never the hostname:
			// re-resolving here would reopen the DNS-rebinding window between
			// the check above and the connection.
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
		},
	}

	return &cimdResolver{
		client: &http.Client{
			Transport: transport,
			Timeout:   cimdFetchTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// The draft requires the AS to treat a redirect as a fetch
				// failure, not follow it.
				return http.ErrUseLastResponse
			},
		},
		cache: make(map[string]cimdCacheEntry),
	}
}

// blockedIPNets are the RFC 6890 special-use ranges that Go's own IP
// predicates do not already cover. They matter here because a Client ID
// Metadata Document URL is supplied by the caller: without this filter, a
// client_id is a primitive for reaching infrastructure that is only
// addressable from the server.
//
// 64:ff9b::/96 is the significant one — NAT64 embeds an arbitrary IPv4
// address in the low 32 bits, so without it a v6 literal can still reach
// 127.0.0.1 or a private range.
var blockedIPNets = func() []*net.IPNet {
	cidrs := []string{
		"100.64.0.0/10",   // RFC 6598 shared address space (CGNAT, some cluster networks)
		"192.0.0.0/24",    // RFC 6890 IETF protocol assignments
		"192.0.2.0/24",    // TEST-NET-1
		"198.18.0.0/15",   // RFC 2544 benchmarking
		"198.51.100.0/24", // TEST-NET-2
		"203.0.113.0/24",  // TEST-NET-3
		"240.0.0.0/4",     // reserved for future use
		"64:ff9b::/96",    // RFC 6052 NAT64 — embeds an arbitrary IPv4 address
		"2002::/16",       // 6to4 — likewise wraps an IPv4 address
		"100::/64",        // RFC 6666 discard-only
	}
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		if _, n, err := net.ParseCIDR(cidr); err == nil {
			nets = append(nets, n)
		}
	}
	return nets
}()

// isSpecialUseIP reports whether ip falls in an RFC 6890 special-use range
// that the draft requires an authorization server to refuse to fetch from.
// IPv4-mapped IPv6 addresses are normalized first, so ::ffff:127.0.0.1 is
// rejected for the same reason 127.0.0.1 is.
func isSpecialUseIP(ip net.IP) bool {
	if ip == nil {
		return true // unparseable: fail closed
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	if ip.Equal(net.IPv4bcast) {
		return true
	}
	for _, n := range blockedIPNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func (r *cimdResolver) IsClientIDURL(clientID string) bool {
	_, err := parseClientIDURL(clientID)
	return err == nil
}

// parseClientIDURL validates clientID against the draft's §2 requirements
// for a Client Identifier URL.
func parseClientIDURL(clientID string) (*url.URL, error) {
	if !strings.HasPrefix(clientID, "https://") {
		return nil, fmt.Errorf("not an https URL")
	}
	u, err := url.Parse(clientID)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("scheme must be https")
	}
	if u.User != nil {
		return nil, fmt.Errorf("must not contain a userinfo component")
	}
	if u.Path == "" {
		return nil, fmt.Errorf("must contain a path component")
	}
	if u.Fragment != "" {
		return nil, fmt.Errorf("must not contain a fragment component")
	}
	for seg := range strings.SplitSeq(u.Path, "/") {
		if seg == "." || seg == ".." {
			return nil, fmt.Errorf("must not contain dot path segments")
		}
	}
	return u, nil
}

func (r *cimdResolver) Resolve(ctx context.Context, clientID string) (*ClientMetadata, error) {
	if _, err := parseClientIDURL(clientID); err != nil {
		return nil, fmt.Errorf("cimd: invalid client_id URL: %w", err)
	}

	r.mu.Lock()
	if entry, ok := r.cache[clientID]; ok && time.Now().Before(entry.expiresAt) {
		r.mu.Unlock()
		return entry.metadata, nil
	}
	r.mu.Unlock()

	metadata, ttl, err := r.fetch(ctx, clientID)
	if err != nil {
		// The draft forbids caching error/invalid responses.
		return nil, err
	}

	r.mu.Lock()
	r.cache[clientID] = cimdCacheEntry{metadata: metadata, expiresAt: time.Now().Add(ttl)}
	r.mu.Unlock()

	return metadata, nil
}

func (r *cimdResolver) fetch(ctx context.Context, clientID string) (*ClientMetadata, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")

	// CodeQL: go/request-forgery ("Uncontrolled data used in network request").
	// Dismissed as "won't fix — risk is mitigated". Rationale, so the decision
	// is reviewable rather than folklore:
	//
	// 1. The finding is accurate. clientID is caller-supplied and does reach
	//    this request's URL. It cannot be otherwise: under
	//    draft-ietf-oauth-client-id-metadata-document the client_id *is* the
	//    URL the authorization server fetches. There is no variant of this
	//    feature where the URL is not attacker-influenced, so the taint path
	//    cannot be broken — only bounded.
	//
	// 2. What bounds it is this client's transport, not this call site, which
	//    is why dataflow analysis cannot see it. NewCIMDResolver's dialer
	//    resolves DNS itself and refuses the host outright if ANY resolved
	//    address is special-use (isSpecialUseIP + blockedIPNets, covering
	//    loopback, private, link-local, CGNAT, and the NAT64/6to4 ranges that
	//    smuggle an IPv4 address inside an IPv6 one). It then dials the address
	//    it validated rather than the hostname, leaving no window to
	//    re-resolve, and never follows redirects. parseClientIDURL has already
	//    constrained the URL to https with no userinfo, fragment or
	//    dot-segments, and the response is capped at 5KB.
	//
	// 3. Residual risk accepted: the server will still make an outbound GET to
	//    an arbitrary *public* host. That is the documented behaviour of the
	//    spec, is rate-bounded by the 5s timeout and the success cache, and is
	//    equivalent to any OIDC/webhook discovery fetch.
	//
	// Do not weaken NewCIMDResolver's transport without revisiting this
	// dismissal — the justification above is the only thing standing between
	// this call and a genuine SSRF primitive.
	//
	// The directive below records the same decision in the SARIF. Note it does
	// not by itself clear the code-scanning gate: GitHub does not act on SARIF
	// suppressions unless a workflow step consumes them, so the alert is also
	// dismissed in the Security tab.
	// codeql[go/request-forgery]
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("cimd: fetch failed: %w", err)
	}
	defer resp.Body.Close()

	// The draft requires exactly 200 OK; redirects (blocked above) and any
	// other status MUST be treated as an error.
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("cimd: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, cimdMaxBodyBytes))
	if err != nil {
		return nil, 0, fmt.Errorf("cimd: reading body: %w", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, 0, fmt.Errorf("cimd: invalid JSON: %w", err)
	}

	// These fields are explicitly forbidden by the draft: a CIMD client is
	// never a confidential client authenticated by a shared secret.
	if _, has := doc["client_secret"]; has {
		return nil, 0, fmt.Errorf("cimd: client_secret is not permitted in a client metadata document")
	}
	if _, has := doc["client_secret_expires_at"]; has {
		return nil, 0, fmt.Errorf("cimd: client_secret_expires_at is not permitted in a client metadata document")
	}

	docClientID, _ := doc["client_id"].(string)
	if docClientID != clientID {
		return nil, 0, fmt.Errorf("cimd: document client_id %q does not match fetch URL %q", docClientID, clientID)
	}

	metadata := &ClientMetadata{ClientID: docClientID}
	if authMethod, ok := doc["token_endpoint_auth_method"].(string); ok {
		switch authMethod {
		case "client_secret_post", "client_secret_basic", "client_secret_jwt":
			return nil, 0, fmt.Errorf("cimd: token_endpoint_auth_method %q is not permitted in a client metadata document", authMethod)
		}
		metadata.TokenEndpointAuthMethod = authMethod
	}
	if rawURIs, ok := doc["redirect_uris"].([]any); ok {
		for _, item := range rawURIs {
			if s, ok := item.(string); ok && s != "" {
				metadata.RedirectURIs = append(metadata.RedirectURIs, s)
			}
		}
	}

	return metadata, cacheTTLFromResponse(resp), nil
}

// cacheTTLFromResponse honors the document's Cache-Control: max-age when
// present, clamped to a sane range, and otherwise falls back to a default.
func cacheTTLFromResponse(resp *http.Response) time.Duration {
	cc := resp.Header.Get("Cache-Control")
	for directive := range strings.SplitSeq(cc, ",") {
		directive = strings.TrimSpace(directive)
		after, ok := strings.CutPrefix(directive, "max-age=")
		if !ok {
			continue
		}
		seconds, err := strconv.Atoi(after)
		if err != nil {
			continue
		}
		ttl := time.Duration(seconds) * time.Second
		if ttl < cimdMinTTL {
			return cimdMinTTL
		}
		if ttl > cimdMaxTTL {
			return cimdMaxTTL
		}
		return ttl
	}
	return cimdDefaultTTL
}
