package resolver

import (
	"context"
	"strings"
	"time"

	"github.com/mavryk-network/dipdup-metadata/internal/ipfs"
)

// Ipfs -
type IpfsNode struct {
	node    *ipfs.Node
	timeout time.Duration
}

// IpfsNodeOption -
type IpfsNodeOption func(*IpfsNode)

// WithTimeoutIpfsNode -
func WithTimeoutIpfsNode(timeout uint64) IpfsNodeOption {
	return func(s *IpfsNode) {
		s.timeout = time.Duration(timeout) * time.Second
	}
}

// NewIPFSNode -
func NewIPFSNode(node *ipfs.Node, opts ...IpfsNodeOption) (IpfsNode, error) {
	s := IpfsNode{
		node: node,
	}

	for i := range opts {
		opts[i](&s)
	}

	return s, nil
}

// Resolve -
func (s IpfsNode) Resolve(ctx context.Context, network, address, link string) (ipfs.Data, error) {
	requestCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	return s.node.Get(requestCtx, strings.TrimPrefix(link, prefixIpfs))
}

// Is -
func (s IpfsNode) Is(link string) bool {
	return strings.HasPrefix(link, prefixIpfs)
}
