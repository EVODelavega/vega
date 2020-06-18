package events

import (
	"context"

	types "code.vegaprotocol.io/vega/proto"
)

type Party struct {
	*Base
	p types.Party
}

func NewPartyEvent(ctx context.Context, p types.Party) *Party {
	return &Party{
		Base: newBase(ctx, PartyEvent),
		p:    p,
	}
}

func (p *Party) Party() types.Party {
	return p.p
}
