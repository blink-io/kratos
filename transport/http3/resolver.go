package http3

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/go-kratos/kratos/v3/internal/endpoint"
	"github.com/go-kratos/kratos/v3/internal/subset"
	"github.com/go-kratos/kratos/v3/log"
	"github.com/go-kratos/kratos/v3/registry"
	"github.com/go-kratos/kratos/v3/selector"
)

// Target is resolver target. HTTP/3 only supports the https scheme.
type Target struct {
	Scheme    string
	Authority string
	Endpoint  string
}

func parseTarget(endpoint string) (*Target, error) {
	if !strings.Contains(endpoint, "://") {
		// HTTP/3 is always over QUIC which mandates TLS, so the URL defaults
		// to https when no scheme is supplied.
		endpoint = schemeHTTPS + "://" + endpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	target := &Target{Scheme: u.Scheme, Authority: u.Host}
	if len(u.Path) > 1 {
		target.Endpoint = u.Path[1:]
	}
	return target, nil
}

type resolver struct {
	rebalancer selector.Rebalancer

	target      *Target
	watcher     registry.Watcher
	selectorKey string
	subsetSize  int
}

func newResolver(ctx context.Context, discovery registry.Discovery, target *Target,
	rebalancer selector.Rebalancer, block bool, subsetSize int,
) (*resolver, error) {
	watcher, err := discovery.Watch(ctx, target.Endpoint)
	if err != nil {
		return nil, err
	}
	r := &resolver{
		target:      target,
		watcher:     watcher,
		rebalancer:  rebalancer,
		selectorKey: uuid.New().String(),
		subsetSize:  subsetSize,
	}
	if block {
		done := make(chan error, 1)
		go func() {
			for {
				services, err := watcher.Next()
				if err != nil {
					done <- err
					return
				}
				if r.update(services) {
					done <- nil
					return
				}
			}
		}()
		select {
		case err := <-done:
			if err != nil {
				stopErr := watcher.Stop()
				if stopErr != nil {
					log.Error("failed to stop http3 client watcher", "target", target, "error", stopErr)
				}
				return nil, err
			}
		case <-ctx.Done():
			log.Error("http3 client watch service reached context deadline", "target", target)
			stopErr := watcher.Stop()
			if stopErr != nil {
				log.Error("failed to stop http3 client watcher", "target", target, "error", stopErr)
			}
			return nil, ctx.Err()
		}
	}
	go func() {
		for {
			services, err := watcher.Next()
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				log.Error("http3 client watch service got unexpected error", "target", target, "error", err)
				time.Sleep(time.Second)
				continue
			}
			r.update(services)
		}
	}()
	return r, nil
}

func (r *resolver) update(services []*registry.ServiceInstance) bool {
	filtered := make([]*registry.ServiceInstance, 0, len(services))
	for _, ins := range services {
		ept, err := endpoint.ParseEndpoint(ins.Endpoints, endpoint.Scheme(schemeHTTPS, true))
		if err != nil {
			log.Error("failed to parse discovery endpoint", "target", r.target, "endpoints", ins.Endpoints, "error", err)
			continue
		}
		if ept == "" {
			continue
		}
		filtered = append(filtered, ins)
	}
	if r.subsetSize != 0 {
		filtered = subset.Subset(r.selectorKey, filtered, r.subsetSize)
	}
	nodes := make([]selector.Node, 0, len(filtered))
	for _, ins := range filtered {
		ept, _ := endpoint.ParseEndpoint(ins.Endpoints, endpoint.Scheme(schemeHTTPS, true))
		nodes = append(nodes, selector.NewNode(schemeHTTPS, ept, ins))
	}

	if len(nodes) == 0 {
		log.Warn("[http3 resolver] zero endpoint found, refused to write", "endpoint", r.target.Endpoint, "nodes", nodes)
		return false
	}
	r.rebalancer.Apply(nodes)
	return true
}

func (r *resolver) Close() error {
	return r.watcher.Stop()
}
