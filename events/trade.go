package events

import (
	"context"

	types "code.vegaprotocol.io/vega/proto"
	eventspb "code.vegaprotocol.io/vega/proto/events/v1"
)

type Trade struct {
	*Base
	t types.Trade
}

func NewTradeEvent(ctx context.Context, t types.Trade) *Trade {
	cpy := t.DeepClone()
	return &Trade{
		Base: newBase(ctx, TradeEvent),
		t:    *cpy,
	}
}

func (t Trade) MarketID() string {
	return t.t.MarketId
}

func (t Trade) IsParty(id string) bool {
	return t.t.Buyer == id || t.t.Seller == id
}

func (t *Trade) Trade() types.Trade {
	return t.t
}

func (t Trade) Proto() types.Trade {
	return t.t
}

func (t Trade) StreamMessage() *eventspb.BusEvent {
	return &eventspb.BusEvent{
		Id:    t.eventID(),
		Block: t.TraceID(),
		Type:  t.et.ToProto(),
		Event: &eventspb.BusEvent_Trade{
			Trade: &t.t,
		},
	}
}
