package subscribers

import (
	"context"
	"sync"

	"code.vegaprotocol.io/vega/events"
	types "code.vegaprotocol.io/vega/proto"
)

type EventFilter func(events.Event) bool

type StreamEvent interface {
	events.Event
	StreamMessage() *types.BusEvent
}

type StreamSub struct {
	*Base
	mu          *sync.Mutex // pointer because types is a value receiver, linter complains
	types       []events.Type
	data        []events.Event
	filters     []EventFilter
	changeCount int
	updated     chan struct{}
}

func NewStreamSub(ctx context.Context, types []events.Type, filters ...EventFilter) *StreamSub {
	trades := false
	expandedTypes := make([]events.Type, 0, len(types))
	for _, t := range types {
		if t == events.MarketEvent {
			expandedTypes = append(expandedTypes, events.MarketEvents()...)
		} else {
			if t == events.TradeEvent {
				trades = true
			}
			expandedTypes = append(expandedTypes, t)
		}
	}
	bufLen := len(expandedTypes) * 10 // each type adds a buffer of 10
	if bufLen == 0 {
		// we're subscribing to all events, buffer should be way more than 0, obviously
		// there's roughly 20 event types, each need a buffer of at least 10
		// trades are a special case: there's potentially hundreds of trade events per block
		// wo we want to ensure our buffers are large enough for a normal trade volume
		trades = true
		bufLen = 200 // 20 event types, buffer of 10 each
	}
	if trades {
		bufLen += 100 // add buffer for 100 events
	}
	s := &StreamSub{
		Base:    NewBase(ctx, bufLen, false),
		mu:      &sync.Mutex{},
		types:   expandedTypes,
		data:    []events.Event{},
		filters: filters,
		updated: make(chan struct{}), // create a blocking channel for these
	}
	if s.isRunning() {
		go s.loop(s.ctx)
	}
	return s
}

func (s *StreamSub) Halt() {
	s.mu.Lock()
	if s.changeCount == 0 {
		close(s.updated)
	}
	s.mu.Unlock()
	s.Base.Halt()
}

func (s *StreamSub) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			s.Halt()
			return
		case e := <-s.ch:
			if s.isRunning() {
				s.Push(e)
			}
		}
	}
}

func (s *StreamSub) Push(evts ...events.Event) {
	if len(evts) == 0 {
		return
	}
	s.mu.Lock()
	closeUpdate := (s.changeCount == 0)
	save := make([]events.Event, 0, len(evts))
	for _, e := range evts {
		keep := true
		for _, f := range s.filters {
			if !f(e) {
				keep = false
				break
			}
		}
		if keep {
			save = append(save, e)
		}
	}
	s.changeCount += len(save)
	if closeUpdate && s.changeCount > 0 {
		close(s.updated)
	}
	s.data = append(s.data, save...)
	s.mu.Unlock()
}

func (s *StreamSub) GetData() []*types.BusEvent {
	<-s.updated
	s.mu.Lock()
	// create a new update channel + reset update counter
	s.updated = make(chan struct{})
	s.changeCount = 0
	// copy the data for return, clear the internal slice
	data := s.data
	s.data = make([]events.Event, 0, cap(data))
	s.mu.Unlock()
	messages := make([]*types.BusEvent, 0, len(data))
	for _, d := range data {
		if se, ok := d.(StreamEvent); ok {
			messages = append(messages, se.StreamMessage())
		}
	}
	return messages
}

func (s StreamSub) Types() []events.Type {
	return s.types
}
