package auth

import "strings"

// allowedNativeRedirectSchemes are the private-use URI schemes belonging to
// native MCP clients that may be used as an OAuth redirect_uri without the
// client having registered it first.
//
// Custom schemes cannot be validated the way an https redirect can — there is
// no origin to compare against — so the only options are an allowlist or
// accepting anything. Accepting anything makes redirect_uri validation
// meaningless: a caller can simply present an unrecognized client_id and send
// the authorization code to a scheme of their choosing. This list keeps the
// editors that actually integrate working while denying that.
//
// A client that registers its redirect_uris (RFC 7591) or publishes them in a
// Client ID Metadata Document is matched exactly against those instead and
// never consults this list, so a new client can always onboard without a code
// change here.
var allowedNativeRedirectSchemes = map[string]bool{
	"cursor":          true,
	"vscode":          true,
	"vscode-insiders": true,
	"vscodium":        true,
	"code-oss":        true,
	"windsurf":        true,
	"zed":             true,
	"jetbrains":       true,
	"claude":          true,
	"claude-code":     true,
}

// IsAllowedNativeRedirectScheme reports whether scheme is a known native-client
// scheme acceptable as an unregistered redirect_uri. Comparison is
// case-insensitive because URI schemes are (RFC 3986 §3.1).
func IsAllowedNativeRedirectScheme(scheme string) bool {
	return allowedNativeRedirectSchemes[strings.ToLower(scheme)]
}
