// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package idgen

import (
	"fmt"

	"github.com/mustafaturan/monoflake"
)

type Service interface {
	NextID() int64
}

type service struct {
	mf *monoflake.MonoFlake
}

// New returns a generator for nodeID, which must be below MaxNodes: monoflake
// keeps only its low 10 bits, so node 1025 would quietly mint node 1's ids.
// The error says 1-1023 because a configured 0 means unset.
func New(nodeID uint16) (Service, error) {
	if nodeID >= MaxNodes {
		return nil, fmt.Errorf("node %d is out of range: it must be 1-%d", nodeID, MaxNodes-1)
	}
	mf, err := monoflake.New(nodeID)
	if err != nil {
		return nil, err
	}
	return &service{mf: mf}, nil
}

func (s *service) NextID() int64 {
	return s.mf.Next().Int64()
}
