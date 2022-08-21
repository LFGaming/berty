package tinder

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/libp2p/go-libp2p-core/peer"
	"go.uber.org/zap"
)

type Subscription struct {
	cancel  context.CancelFunc
	ctx     context.Context
	service *Service
	topic   string
	out     <-chan peer.AddrInfo
}

func (s *Subscription) Out() <-chan peer.AddrInfo {
	return s.out
}

func (s *Subscription) Pull() error {
	return s.service.LookupPeers(s.ctx, s.topic)
}

func (s *Subscription) Close() error {
	s.cancel()
	return nil
}

func (s *Service) Subscribe(topic string) *Subscription {
	ctx, cancel := context.WithCancel(context.Background())
	out := s.fadeOut(ctx, topic, 16)
	go func() {
		if err := s.WatchTopic(ctx, topic); err != nil {
			s.logger.Error("unable to watch topic", zap.String("topic", topic), zap.Error(err))
		}
	}()

	return &Subscription{
		service: s,
		out:     out,
		ctx:     ctx,
		cancel:  cancel,
		topic:   topic,
	}
}

func (s *Service) LookupPeers(ctx context.Context, topic string) error {
	var wg sync.WaitGroup
	var success int32

	for _, d := range s.drivers {
		wg.Add(1)
		go func(d IDriver) {
			in, err := d.FindPeers(ctx, topic) // find peers should not hang there
			switch err {
			case nil: // ok
				atomic.AddInt32(&success, 1)
				s.logger.Debug("lookup for topic started", zap.String("driver", d.Name()), zap.String("topic", topic))
				s.fadeIn(ctx, topic, in)
			case ErrNotSupported: // do nothing
			default:
				s.logger.Error("lookup failed",
					zap.String("driver", d.Name()), zap.String("topic", topic), zap.Error(err))
			}

			wg.Done()
		}(d)

	}

	wg.Wait() // wait for process to finish

	if success == 0 {
		return fmt.Errorf("no driver(s) were available for lookup")
	}

	return nil
}

func (s *Service) WatchTopic(ctx context.Context, topic string) error {
	var wg sync.WaitGroup
	var success int32

	for _, d := range s.drivers {
		s.logger.Debug("start subscribe", zap.String("driver", d.Name()), zap.String("topic", topic))

		wg.Add(1)
		go func(d IDriver) {
			in, err := d.Subscribe(ctx, topic)
			switch err {
			case nil: // ok, start fadin
				atomic.AddInt32(&success, 1)
				s.logger.Debug("watching for topic update", zap.String("driver", d.Name()), zap.String("topic", topic))
				s.fadeIn(ctx, topic, in)
			case ErrNotSupported: // not, supported skipping
			default:
				s.logger.Error("unable to subscribe on topic",
					zap.String("driver", d.Name()), zap.String("topic", topic), zap.Error(err))
			}

			wg.Done()
		}(d)
	}

	wg.Wait() // wait for process to finish

	if success == 0 {
		return fmt.Errorf("no driver(s) were available for subscribe")
	}

	return nil
}
