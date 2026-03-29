package idgen

import (
	"github.com/mustafaturan/monoflake"
)

type Service interface {
	NextID() int64
}

type service struct {
	mf *monoflake.MonoFlake
}

func New(nodeID uint16) (Service, error) {
	mf, err := monoflake.New(nodeID)
	if err != nil {
		return nil, err
	}
	return &service{mf: mf}, nil
}

func (s *service) NextID() int64 {
	return s.mf.Next().Int64()
}
