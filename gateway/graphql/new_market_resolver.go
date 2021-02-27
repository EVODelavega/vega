package gql

import (
	"context"
	"errors"

	"code.vegaprotocol.io/vega/proto"
	types "code.vegaprotocol.io/vega/proto"
)

type newMarketResolver VegaResolverRoot

func (r *newMarketResolver) Instrument(ctx context.Context, obj *types.NewMarket) (*types.InstrumentConfiguration, error) {
	return obj.Changes.Instrument, nil
}

func (r *newMarketResolver) DecimalPlaces(ctx context.Context, obj *types.NewMarket) (int, error) {
	return int(obj.Changes.DecimalPlaces), nil
}

func (r *newMarketResolver) RiskParameters(ctx context.Context, obj *types.NewMarket) (RiskModel, error) {
	switch rm := obj.Changes.RiskParameters.(type) {
	case *proto.NewMarketConfiguration_LogNormal:
		return rm.LogNormal, nil
	case *proto.NewMarketConfiguration_Simple:
		return rm.Simple, nil
	default:
		return nil, errors.New("invalid risk model")
	}
}

func (r *newMarketResolver) Metadata(ctx context.Context, obj *types.NewMarket) ([]string, error) {
	return obj.Changes.Metadata, nil
}

func (r *newMarketResolver) TradingMode(ctx context.Context, obj *types.NewMarket) (TradingMode, error) {
	return NewMarketTradingModeFromProto(obj.Changes.TradingMode)
}
