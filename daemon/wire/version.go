// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package wire

import (
	"regexp"
	"strconv"
	"strings"
)

// MinForkVersion is the first agentrqd release that runs a workspace fork.
// A daemon must be at least this and say [CapabilityFork] as well: a
// development build can say the capability without making fork folders.
const MinForkVersion = "0.9.3"

// MinRemoteControlVersion is the first agentrqd release that can be restarted
// or updated from the panel and bring its agents back listed. Before it an
// update never came back under systemd or launchd, and the agents it restored
// had lost their session rows.
const MinRemoteControlVersion = "0.9.3"

var releasePattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`)

// VersionAtLeast reports whether a daemon's [Hello.Version] is min or later,
// compared as semver, so 0.9.10 is later than 0.9.3. min must be a release.
//
// A pre-release is below its release, and a version that is not a release
// ("dev", or empty) is below everything: a floor exists to refuse what it
// cannot vouch for.
func VersionAtLeast(version, min string) bool {
	v, vPre, ok := parseRelease(version)
	m, _, _ := parseRelease(min)
	if !ok {
		return false
	}
	for i := range v {
		if v[i] != m[i] {
			return v[i] > m[i]
		}
	}
	return !vPre
}

func parseRelease(s string) (core [3]int, pre bool, ok bool) {
	if !releasePattern.MatchString(s) {
		return core, false, false
	}
	base, _, pre := strings.Cut(s, "-")
	for i, part := range strings.Split(base, ".") {
		n, err := strconv.Atoi(part)
		if err != nil {
			return core, false, false // too large for an int
		}
		core[i] = n
	}
	return core, pre, true
}
