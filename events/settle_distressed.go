package events

import "context"

type SettleDistressed struct {
	*Base
	partyID  string
	marketID string
	margin   uint64
	price    uint64
}

func NewSettleDistressed(ctx context.Context, partyID, marketID string, price, margin uint64) *SettleDistressed {
	return &SettleDistressed{
		Base:     newBase(ctx, SettleDistressedEvent),
		partyID:  partyID,
		marketID: marketID,
		margin:   margin,
		price:    price,
	}
}

func (s SettleDistressed) PartyID() string {
	return s.partyID
}

func (s SettleDistressed) MarketID() string {
	return s.marketID
}

func (s SettleDistressed) Margin() uint64 {
	return s.margin
}

func (s SettleDistressed) Price() uint64 {
	return s.price
}
