package subscribers_test

import (
	"context"
	"sync"
	"testing"

	"code.vegaprotocol.io/vega/events"
	types "code.vegaprotocol.io/vega/proto"
	"code.vegaprotocol.io/vega/subscribers"

	"github.com/stretchr/testify/assert"
)

type tstStreamSub struct {
	*subscribers.StreamSub
	ctx   context.Context
	cfunc context.CancelFunc
}

type accEvt interface {
	events.Event
	Account() types.Account
}

func getTestStreamSub(types []events.Type, filters ...subscribers.EventFilter) *tstStreamSub {
	ctx, cfunc := context.WithCancel(context.Background())
	return &tstStreamSub{
		StreamSub: subscribers.NewStreamSub(ctx, types, filters...),
		ctx:       ctx,
		cfunc:     cfunc,
	}
}

func accMarketIDFilter(mID string) subscribers.EventFilter {
	return func(e events.Event) bool {
		ae, ok := e.(accEvt)
		if !ok {
			return false
		}
		if ae.Account().MarketID != mID {
			return false
		}
		return true
	}
}

func TestUnfilteredSubscription(t *testing.T) {
	t.Run("Stream subscriber without filters, no events", testUnfilteredNoEvents)
	t.Run("Stream subscriber without filters - with events", testUnfilteredWithEventsPush)
}

func TestFilteredSubscription(t *testing.T) {
	t.Run("Stream subscriber with filter - no valid events", testFilteredNoValidEvents)
	t.Run("Stream subscriber with filter - some valid events", testFilteredSomeValidEvents)
}

func TestSubscriberTypes(t *testing.T) {
	t.Run("Stream subscriber for all event types", testFilterAll)
}

func TestMidChannelDone(t *testing.T) {
	t.Run("Stream subscriber stops mid event stream", testCloseChannelWrite)
}

func testUnfilteredNoEvents(t *testing.T) {
	sub := getTestStreamSub([]events.Type{events.AccountEvent})
	wg := sync.WaitGroup{}
	wg.Add(1)
	var data []*types.BusEvent
	go func() {
		data = sub.GetData()
		wg.Done()
	}()
	sub.cfunc() // cancel ctx
	wg.Wait()
	// we expect to see no events
	assert.Equal(t, 0, len(data))
}

func testUnfilteredWithEventsPush(t *testing.T) {
	sub := getTestStreamSub([]events.Type{events.AccountEvent})
	defer sub.cfunc()
	set := []events.Event{
		events.NewAccountEvent(sub.ctx, types.Account{
			Id: "acc-1",
		}),
		events.NewAccountEvent(sub.ctx, types.Account{
			Id: "acc-2",
		}),
	}
	sub.Push(set...)
	data := sub.GetData()
	// we expect to see no events
	assert.Equal(t, len(set), len(data))
	last := events.NewAccountEvent(sub.ctx, types.Account{
		Id: "acc-3",
	})
	sub.Push(last)
	data = sub.GetData()
	assert.Equal(t, 1, len(data))
	rt, err := events.ProtoToInternal(data[0].Type)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(rt))
	assert.Equal(t, events.AccountEvent, rt[0])
	acc := data[0].GetAccount()
	assert.NotNil(t, acc)
	assert.Equal(t, last.Account().Id, acc.Id)
}

func testFilteredNoValidEvents(t *testing.T) {
	sub := getTestStreamSub([]events.Type{events.AccountEvent}, accMarketIDFilter("valid"))
	set := []events.Event{
		events.NewAccountEvent(sub.ctx, types.Account{
			Id:       "acc-1",
			MarketID: "invalid",
		}),
		events.NewAccountEvent(sub.ctx, types.Account{
			Id:       "acc-2",
			MarketID: "also-invalid",
		}),
	}
	sub.Push(set...)
	wg := sync.WaitGroup{}
	wg.Add(1)
	var data []*types.BusEvent
	go func() {
		data = sub.GetData()
		wg.Done()
	}()
	sub.cfunc()
	wg.Wait()
	// we expect to see no events
	assert.Equal(t, 0, len(data))
}

func testFilteredSomeValidEvents(t *testing.T) {
	sub := getTestStreamSub([]events.Type{events.AccountEvent}, accMarketIDFilter("valid"))
	defer sub.cfunc()
	set := []events.Event{
		events.NewAccountEvent(sub.ctx, types.Account{
			Id:       "acc-1",
			MarketID: "invalid",
		}),
		events.NewAccountEvent(sub.ctx, types.Account{
			Id:       "acc-2",
			MarketID: "valid",
		}),
	}
	sub.Push(set...)
	data := sub.GetData()
	// we expect to see no events
	assert.Equal(t, 1, len(data))
}

func testFilterAll(t *testing.T) {
	sub := getTestStreamSub([]events.Type{events.All})
	assert.Nil(t, sub.Types())
}

// this test aims to replicate the crash when trying to write to a closed channel
func testCloseChannelWrite(t *testing.T) {
	mID := "tstMarket"
	sub := getTestStreamSub([]events.Type{events.AccountEvent}, accMarketIDFilter(mID))
	set := []events.Event{
		events.NewAccountEvent(sub.ctx, types.Account{
			Id:       "acc1",
			MarketID: mID,
		}),
		events.NewAccountEvent(sub.ctx, types.Account{
			Id:       "acc2",
			MarketID: mID,
		}),
		events.NewAccountEvent(sub.ctx, types.Account{
			Id:       "acc50",
			MarketID: "other-market",
		}),
		events.NewAccountEvent(sub.ctx, types.Account{
			Id:       "acc3",
			MarketID: mID,
		}),
		events.NewAccountEvent(sub.ctx, types.Account{
			Id:       "acc4",
			MarketID: mID,
		}),
		events.NewAccountEvent(sub.ctx, types.Account{
			Id:       "acc51",
			MarketID: "other-market",
		}),
		events.NewAccountEvent(sub.ctx, types.Account{
			Id:       "acc5",
			MarketID: "other-market",
		}),
		events.NewAccountEvent(sub.ctx, types.Account{
			Id:       "acc6",
			MarketID: mID,
		}),
		events.NewAccountEvent(sub.ctx, types.Account{
			Id:       "acc7",
			MarketID: mID,
		}),
	}
	started := make(chan struct{})
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		first := false
		defer wg.Done()
		// keep iterating until the context was closed, ensuring
		// the context is cancelled mid-send
		for {
			for _, e := range set {
				// ch := sub.C()
				select {
				case <-sub.Closed():
					return
				case <-sub.Skip():
					return
				case sub.C() <- e:
					// case ch <- e:
					if !first {
						first = true
						close(started)
					}
				}
			}
		}
	}()
	<-started
	sub.cfunc()
	// wait for sub to be confirmed closed down
	wg.Wait()
	data := sub.GetData()
	// we received at least the first event, which is valid (filtered)
	// so this slice ought not to be empty
	assert.NotEmpty(t, data)
}
