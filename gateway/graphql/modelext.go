package gql

import (
	"fmt"

	types "code.vegaprotocol.io/vega/proto"
	protoapi "code.vegaprotocol.io/vega/proto/api"
	"github.com/pkg/errors"
)

var (
	// ErrNilTradingMode ...
	ErrNilTradingMode = errors.New("nil trading mode")
	// ErrAmbiguousTradingMode ...
	ErrAmbiguousTradingMode = errors.New("more than one trading mode selected")
	// ErrUnimplementedTradingMode ...
	ErrUnimplementedTradingMode = errors.New("unimplemented trading mode")
	// ErrNilMarket ...
	ErrNilMarket = errors.New("nil market")
	// ErrNilTradableInstrument ...
	ErrNilTradableInstrument = errors.New("nil tradable instrument")
	// ErrNilOracle ..
	ErrNilOracle = errors.New("nil oracle")
	// ErrUnimplementedOracle ...
	ErrUnimplementedOracle = errors.New("unimplemented oracle")
	// ErrNilProduct ...
	ErrNilProduct = errors.New("nil product")
	// ErrUnimplementedProduct ...
	ErrUnimplementedProduct = errors.New("unimplemented product")
	// ErrNilRiskModel ...
	ErrNilRiskModel = errors.New("nil risk model")
	// ErrUnimplementedRiskModel ...
	ErrUnimplementedRiskModel = errors.New("unimplemented risk model")
	// ErrNilInstrumentMetadata ...
	ErrNilInstrumentMetadata = errors.New("nil instrument metadata")
	// ErrNilEthereumEvent ...
	ErrNilEthereumEvent = errors.New("nil ethereum event")
	// ErrNilFuture ...
	ErrNilFuture = errors.New("nil future")
	// ErrNilInstrument ...
	ErrNilInstrument = errors.New("nil instrument")
	// ErrTradingDurationNegative ...
	ErrTradingDurationNegative = errors.New("invalid trading duration (negative)")
	// ErrTickSizeNegative ...
	ErrTickSizeNegative = errors.New("invalid tick size (negative)")
	// ErrNilContinuousTradingTickSize ...
	ErrNilContinuousTradingTickSize = errors.New("nil continuous trading tick-size")
	// ErrNilScalingFactors ...
	ErrNilScalingFactors = errors.New("nil scaling factors")
	// ErrNilMarginCalculator ...
	ErrNilMarginCalculator = errors.New("nil margin calculator")
	// ErrInvalidTickSize ...
	ErrInvalidTickSize = errors.New("invalid tick size")
	// ErrInvalidDecimalPlaces ...
	ErrInvalidDecimalPlaces = errors.New("invalid decimal places value")
	// ErrInvalidChange ...
	ErrInvalidChange = errors.New("nil update market, new market and update network")
	// ErrInvalidProposalState ...
	ErrInvalidProposalState = errors.New("invalid proposal state")
	// ErrInvalidRiskConfiguration ...
	ErrInvalidRiskConfiguration = errors.New("invalid risk configuration")
	// ErrNilAssetSource returned when an asset source is not specified at creation
	ErrNilAssetSource = errors.New("nil asset source")
	// ErrUnimplementedAssetSource returned when an asset source specified at creation is not recognised
	ErrUnimplementedAssetSource = errors.New("unimplemented asset source")
	// ErrMultipleProposalChangesSpecified is raised when multiple proposal changes are set
	// (non-null) for a singe proposal terms
	ErrMultipleProposalChangesSpecified = errors.New("multiple proposal changes specified")
	// ErrMultipleAssetSourcesSpecified is raised when multiple asset source are specified
	ErrMultipleAssetSourcesSpecified = errors.New("multiple asset sources specified")
)

// IntoProto ...
func (c *ContinuousTrading) IntoProto() (*types.Market_Continuous, error) {
	if c.TickSize < 0 {
		return nil, ErrTickSizeNegative
	}
	return &types.Market_Continuous{
		Continuous: &types.ContinuousTrading{
			TickSize: uint64(c.TickSize),
		},
	}, nil
}

// IntoProto ...
func (d *DiscreteTrading) IntoProto() (*types.Market_Discrete, error) {
	if d.TickSize < 0 {
		return nil, ErrTickSizeNegative
	}
	if d.Duration < 0 {
		return nil, ErrTradingDurationNegative
	}
	return &types.Market_Discrete{
		Discrete: &types.DiscreteTrading{
			TickSize:   uint64(d.TickSize),
			DurationNs: int64(d.Duration),
		},
	}, nil
}

func (m *Market) tradingModeIntoProto(mkt *types.Market) (err error) {
	if m.TradingMode == nil {
		return ErrNilTradingMode
	}
	switch tm := m.TradingMode.(type) {
	case *ContinuousTrading:
		mkt.TradingMode, err = tm.IntoProto()
	case *DiscreteTrading:
		mkt.TradingMode, err = tm.IntoProto()
	default:
		err = ErrUnimplementedTradingMode
	}
	return err
}

// IntoProto ...
func (ee *EthereumEvent) IntoProto() (*types.Future_EthereumEvent, error) {
	return &types.Future_EthereumEvent{
		EthereumEvent: &types.EthereumEvent{
			ContractID: ee.ContractID,
			Event:      ee.Event,
		},
	}, nil
}

func (f *Future) oracleIntoProto(pf *types.Future) (err error) {
	if f.Oracle == nil {
		return ErrNilOracle
	}
	switch o := f.Oracle.(type) {
	case *EthereumEvent:
		pf.Oracle, err = o.IntoProto()
	default:
		err = ErrUnimplementedOracle
	}
	return err

}

// IntoProto ...
func (f *Future) IntoProto() (*types.Instrument_Future, error) {
	var err error
	pf := &types.Future{
		Maturity: f.Maturity,
		Asset:    f.Asset,
	}
	err = f.oracleIntoProto(pf)
	if err != nil {
		return nil, err
	}

	return &types.Instrument_Future{Future: pf}, err
}

// IntoProto ...
func (im *InstrumentMetadata) IntoProto() (*types.InstrumentMetadata, error) {
	pim := &types.InstrumentMetadata{
		Tags: []string{},
	}
	for _, v := range im.Tags {
		pim.Tags = append(pim.Tags, *v)
	}
	return pim, nil
}

func (i *Instrument) productIntoProto(pinst *types.Instrument) (err error) {
	if i.Product == nil {
		return ErrNilProduct
	}
	switch p := i.Product.(type) {
	case *Future:
		pinst.Product, err = p.IntoProto()
	default:
		err = ErrUnimplementedProduct
	}
	return err
}

// IntoProto ...
func (i *Instrument) IntoProto() (*types.Instrument, error) {
	var err error
	pinst := &types.Instrument{
		Id:        i.ID,
		Code:      i.Code,
		Name:      i.Name,
		BaseName:  i.BaseName,
		QuoteName: i.QuoteName,
	}

	if i.Metadata != nil {
		pinst.Metadata, err = i.Metadata.IntoProto()
		if err != nil {
			return nil, err
		}
	}
	err = i.productIntoProto(pinst)
	if err != nil {
		return nil, err
	}

	return pinst, err
}

// IntoProto ...
func (f *LogNormalRiskModel) IntoProto() (*types.TradableInstrument_LogNormalRiskModel, error) {
	return &types.TradableInstrument_LogNormalRiskModel{
		LogNormalRiskModel: &types.LogNormalRiskModel{
			RiskAversionParameter: f.RiskAversionParameter,
			Tau:                   f.Tau,
			Params: &types.LogNormalModelParams{
				Mu:    f.Params.Mu,
				R:     f.Params.R,
				Sigma: f.Params.Sigma,
			},
		},
	}, nil
}

func (ti *TradableInstrument) riskModelIntoProto(
	pti *types.TradableInstrument) (err error) {
	if ti.RiskModel == nil {
		return ErrNilRiskModel
	}
	switch rm := ti.RiskModel.(type) {
	case *LogNormalRiskModel:
		pti.RiskModel, err = rm.IntoProto()
	default:
		err = ErrUnimplementedRiskModel
	}
	return err
}

// IntoProto ...
func (ti *TradableInstrument) IntoProto() (*types.TradableInstrument, error) {
	var err error
	pti := &types.TradableInstrument{}
	if ti.Instrument != nil {
		pti.Instrument, err = ti.Instrument.IntoProto()
		if err != nil {
			return nil, err
		}
	}
	if ti.MarginCalculator != nil {
		pti.MarginCalculator, _ = ti.MarginCalculator.IntoProto()
	}
	err = ti.riskModelIntoProto(pti)
	if err != nil {
		return nil, err
	}

	return pti, nil
}

func (m *MarginCalculator) IntoProto() (*types.MarginCalculator, error) {
	pm := &types.MarginCalculator{}
	if m.ScalingFactors != nil {
		pm.ScalingFactors, _ = m.ScalingFactors.IntoProto()
	}
	return pm, nil
}

func (s *ScalingFactors) IntoProto() (*types.ScalingFactors, error) {
	return &types.ScalingFactors{
		SearchLevel:       s.SearchLevel,
		InitialMargin:     s.InitialMargin,
		CollateralRelease: s.CollateralRelease,
	}, nil
}

// IntoProto ...
func (m *Market) IntoProto() (*types.Market, error) {
	var err error
	pmkt := &types.Market{}
	pmkt.Id = m.ID
	if err = m.tradingModeIntoProto(pmkt); err != nil {
		return nil, err
	}

	if m.TradableInstrument != nil {
		pmkt.TradableInstrument, err = m.TradableInstrument.IntoProto()
		if err != nil {
			return nil, err
		}
	}

	return pmkt, nil
}

// ContinuousTradingFromProto ...
func ContinuousTradingFromProto(pct *types.ContinuousTrading) (*ContinuousTrading, error) {
	return &ContinuousTrading{
		TickSize: int(pct.TickSize),
	}, nil
}

// DiscreteTradingFromProto ...
func DiscreteTradingFromProto(pdt *types.DiscreteTrading) (*DiscreteTrading, error) {
	return &DiscreteTrading{
		Duration: int(pdt.DurationNs),
		TickSize: int(pdt.TickSize),
	}, nil
}

// TradingModeFromProto ...
func TradingModeFromProto(ptm interface{}) (TradingMode, error) {
	if ptm == nil {
		return nil, ErrNilTradingMode
	}

	switch ptmimpl := ptm.(type) {
	case *types.Market_Continuous:
		return ContinuousTradingFromProto(ptmimpl.Continuous)
	case *types.Market_Discrete:
		return DiscreteTradingFromProto(ptmimpl.Discrete)
	default:
		return nil, ErrUnimplementedTradingMode
	}
}

// NewMarketTradingModeFromProto ...
func NewMarketTradingModeFromProto(ptm interface{}) (TradingMode, error) {
	if ptm == nil {
		return nil, ErrNilTradingMode
	}
	switch ptmimpl := ptm.(type) {
	case *types.NewMarketConfiguration_Continuous:
		return ContinuousTradingFromProto(ptmimpl.Continuous)
	case *types.NewMarketConfiguration_Discrete:
		return DiscreteTradingFromProto(ptmimpl.Discrete)
	default:
		return nil, ErrUnimplementedTradingMode
	}
}

// InstrumentMetadataFromProto ...
func InstrumentMetadataFromProto(pim *types.InstrumentMetadata) (*InstrumentMetadata, error) {
	if pim == nil {
		return nil, ErrNilInstrumentMetadata
	}
	im := &InstrumentMetadata{
		Tags: []*string{},
	}

	for _, v := range pim.Tags {
		v := v
		im.Tags = append(im.Tags, &v)
	}

	return im, nil
}

// EthereumEventFromProto ...
func EthereumEventFromProto(pee *types.EthereumEvent) (*EthereumEvent, error) {
	if pee == nil {
		return nil, ErrNilEthereumEvent
	}

	return &EthereumEvent{
		ContractID: pee.ContractID,
		Event:      pee.Event,
	}, nil
}

// OracleFromProto ...
func OracleFromProto(o interface{}) (Oracle, error) {
	if o == nil {
		return nil, ErrNilOracle
	}

	switch oimpl := o.(type) {
	case *types.Future_EthereumEvent:
		return EthereumEventFromProto(oimpl.EthereumEvent)
	default:
		return nil, ErrUnimplementedOracle
	}
}

// FutureFromProto ...
func FutureFromProto(pf *types.Future) (*Future, error) {
	if pf == nil {
		return nil, ErrNilFuture
	}

	var err error
	f := &Future{}
	f.Maturity = pf.Maturity
	f.Asset = pf.Asset
	f.Oracle, err = OracleFromProto(pf.Oracle)
	if err != nil {
		return nil, err
	}

	return f, nil
}

// ProductFromProto ...
func ProductFromProto(pp interface{}) (Product, error) {
	if pp == nil {
		return nil, ErrNilProduct
	}

	switch pimpl := pp.(type) {
	case *types.Instrument_Future:
		return FutureFromProto(pimpl.Future)
	default:
		return nil, ErrUnimplementedProduct
	}
}

// InstrumentFromProto ...
func InstrumentFromProto(pi *types.Instrument) (*Instrument, error) {
	if pi == nil {
		return nil, ErrNilInstrument
	}
	var err error
	i := &Instrument{
		ID:        pi.Id,
		Code:      pi.Code,
		Name:      pi.Name,
		BaseName:  pi.BaseName,
		QuoteName: pi.QuoteName,
	}
	meta, err := InstrumentMetadataFromProto(pi.Metadata)
	if err != nil {
		return nil, err
	}
	i.Metadata = meta
	i.Product, err = ProductFromProto(pi.Product)
	if err != nil {
		return nil, err
	}

	return i, nil
}

// ForwardFromProto ...
func ForwardFromProto(f *types.LogNormalRiskModel) (*LogNormalRiskModel, error) {
	return &LogNormalRiskModel{
		RiskAversionParameter: f.RiskAversionParameter,
		Tau:                   f.Tau,
		Params: &LogNormalModelParams{
			Mu:    f.Params.Mu,
			R:     f.Params.R,
			Sigma: f.Params.Sigma,
		},
	}, nil
}

// SimpleRiskModelFromProto ...
func SimpleRiskModelFromProto(f *types.SimpleRiskModel) (*SimpleRiskModel, error) {
	return &SimpleRiskModel{
		Params: &SimpleRiskModelParams{
			FactorLong:  f.Params.FactorLong,
			FactorShort: f.Params.FactorShort,
		},
	}, nil
}

// RiskModelFromProto ...
func RiskModelFromProto(rm interface{}) (RiskModel, error) {
	if rm == nil {
		return nil, ErrNilRiskModel
	}
	switch rmimpl := rm.(type) {
	case *types.TradableInstrument_LogNormalRiskModel:
		return ForwardFromProto(rmimpl.LogNormalRiskModel)
	case *types.TradableInstrument_SimpleRiskModel:
		return SimpleRiskModelFromProto(rmimpl.SimpleRiskModel)
	default:
		return nil, ErrUnimplementedRiskModel
	}
}

// TradableInstrumentFromProto ...
func TradableInstrumentFromProto(pti *types.TradableInstrument) (*TradableInstrument, error) {
	if pti == nil {
		return nil, ErrNilTradableInstrument
	}
	var err error
	ti := &TradableInstrument{}
	instrument, err := InstrumentFromProto(pti.Instrument)
	if err != nil {
		return nil, err
	}
	ti.Instrument = instrument
	ti.RiskModel, err = RiskModelFromProto(pti.RiskModel)
	if err != nil {
		return nil, err
	}
	mc, err := MarginCalculatorFromProto(pti.MarginCalculator)
	if err != nil {
		return nil, err
	}
	ti.MarginCalculator = mc
	return ti, nil
}

func MarginCalculatorFromProto(mc *types.MarginCalculator) (*MarginCalculator, error) {
	if mc == nil {
		return nil, ErrNilMarginCalculator
	}
	m := &MarginCalculator{}
	sf, err := ScalingFactorsFromProto(mc.ScalingFactors)
	if err != nil {
		return nil, err
	}
	m.ScalingFactors = sf
	return m, nil
}

func ScalingFactorsFromProto(psf *types.ScalingFactors) (*ScalingFactors, error) {
	if psf == nil {
		return nil, ErrNilScalingFactors
	}
	return &ScalingFactors{
		SearchLevel:       psf.SearchLevel,
		InitialMargin:     psf.InitialMargin,
		CollateralRelease: psf.CollateralRelease,
	}, nil
}

// MarketFromProto ...
func MarketFromProto(pmkt *types.Market) (*Market, error) {
	if pmkt == nil {
		return nil, ErrNilMarket
	}
	var err error
	mkt := &Market{}
	mkt.ID = pmkt.Id
	mkt.DecimalPlaces = int(pmkt.DecimalPlaces)
	mkt.TradingMode, err = TradingModeFromProto(pmkt.TradingMode)
	if err != nil {
		return nil, err
	}
	tradableInstrument, err :=
		TradableInstrumentFromProto(pmkt.TradableInstrument)
	if err != nil {
		return nil, err
	}
	mkt.TradableInstrument = tradableInstrument

	return mkt, nil
}
func (i *InstrumentConfiguration) assignProductFromProto(instrument *types.InstrumentConfiguration) error {
	if future := instrument.GetFuture(); future != nil {
		i.FutureProduct = &FutureProduct{
			Asset:    future.Asset,
			Maturity: future.Maturity,
		}
	} else {
		return ErrNilProduct
	}
	return nil
}

// RiskConfigurationFromProto ...
func RiskConfigurationFromProto(newMarket *types.NewMarketConfiguration) (RiskModel, error) {
	switch params := newMarket.RiskParameters.(type) {
	case *types.NewMarketConfiguration_Simple:
		return &SimpleRiskModel{
			Params: &SimpleRiskModelParams{
				FactorLong:  params.Simple.FactorLong,
				FactorShort: params.Simple.FactorShort,
			},
		}, nil
	case *types.NewMarketConfiguration_LogNormal:
		return &LogNormalRiskModel{
			RiskAversionParameter: params.LogNormal.RiskAversionParameter,
			Tau:                   params.LogNormal.Tau,
			Params: &LogNormalModelParams{
				Mu:    params.LogNormal.Params.Mu,
				R:     params.LogNormal.Params.R,
				Sigma: params.LogNormal.Params.Sigma,
			},
		}, nil
	default:
		return nil, ErrInvalidRiskConfiguration
	}
}

// NewMarketFromProto ...
func NewMarketFromProto(newMarket *types.NewMarketConfiguration) (*NewMarket, error) {
	if newMarket == nil {
		return nil, ErrNilMarket
	}
	risk, err := RiskConfigurationFromProto(newMarket)
	if err != nil {
		return nil, err
	}
	mode, err := NewMarketTradingModeFromProto(newMarket.TradingMode)
	if err != nil {
		return nil, err
	}

	result := &NewMarket{
		Instrument: &InstrumentConfiguration{
			Name:      newMarket.Instrument.Name,
			Code:      newMarket.Instrument.Code,
			BaseName:  newMarket.Instrument.BaseName,
			QuoteName: newMarket.Instrument.QuoteName,
		},
		DecimalPlaces:  int(newMarket.DecimalPlaces),
		RiskParameters: risk,
		TradingMode:    mode,
	}
	result.Instrument.assignProductFromProto(newMarket.Instrument)
	return result, nil
}

// ProposalTermsFromProto ...
func ProposalTermsFromProto(terms *types.ProposalTerms) (*ProposalTerms, error) {
	result := &ProposalTerms{
		ClosingDatetime:   secondsTSToDatetime(terms.ClosingTimestamp),
		EnactmentDatetime: secondsTSToDatetime(terms.EnactmentTimestamp),
	}
	if terms.GetUpdateMarket() != nil {
		result.Change = nil
	} else if newMarket := terms.GetNewMarket(); newMarket != nil {
		marketConfig, err := NewMarketFromProto(newMarket.Changes)
		if err != nil {
			return nil, err
		}
		result.Change = marketConfig
	} else if terms.GetUpdateNetwork() != nil {
		result.Change = nil
	} else if newAsset := terms.GetNewAsset(); newAsset != nil {
		newAsset, err := NewAssetFromProto(newAsset)
		if err != nil {
			return nil, err
		}
		result.Change = newAsset

	}
	return result, nil
}

// IntoProto ...
func (i *InstrumentConfigurationInput) IntoProto() (*types.InstrumentConfiguration, error) {
	if len(i.Name) <= 0 {
		return nil, errors.New("Instrument.Name: string cannot be empty")
	}
	if len(i.Code) <= 0 {
		return nil, errors.New("Instrument.Code: string cannot be empty")
	}
	if len(i.BaseName) <= 0 {
		return nil, errors.New("Instrument.BaseName: string cannot be empty")
	}
	if len(i.QuoteName) <= 0 {
		return nil, errors.New("Instrument.QuoteName: string cannot be empty")
	}

	result := &types.InstrumentConfiguration{
		Name:      i.Name,
		Code:      i.Code,
		BaseName:  i.BaseName,
		QuoteName: i.QuoteName,
	}

	if i.FutureProduct != nil {
		if len(i.FutureProduct.Asset) <= 0 {
			return nil, errors.New("FutureProduct.Asset: string cannot be empty")
		}
		if len(i.FutureProduct.Maturity) <= 0 {
			return nil, errors.New("FutureProduct.Maturity: string cannot be empty")
		}

		result.Product = &types.InstrumentConfiguration_Future{
			Future: &types.FutureProduct{
				Asset:    i.FutureProduct.Asset,
				Maturity: i.FutureProduct.Maturity,
			},
		}
	} else {
		return nil, ErrNilProduct
	}
	return result, nil
}

// IntoProto ...
func (l *LogNormalModelParamsInput) IntoProto() (*types.LogNormalModelParams, error) {
	if l.Sigma < 0 {
		return nil, errors.New("LogNormalRiskModelParams.Sigma: needs to be any strictly non-negative float")
	}
	return &types.LogNormalModelParams{
		Mu:    l.Mu,
		R:     l.R,
		Sigma: l.Sigma,
	}, nil
}

// IntoProto ...
func (l *LogNormalRiskModelInput) IntoProto() (*types.NewMarketConfiguration_LogNormal, error) {
	if l.RiskAversionParameter <= 0 || l.RiskAversionParameter >= 1 {
		return nil, errors.New("LogNormalRiskModel.RiskAversionParameter: needs to be strictly greater than 0 and strictly smaller than 1")
	}
	if l.Tau < 0 {
		return nil, errors.New("LogNormalRiskModel.Tau: needs to be any strictly non-negative float")
	}

	params, err := l.Params.IntoProto()
	if err != nil {
		return nil, err
	}

	return &types.NewMarketConfiguration_LogNormal{
		LogNormal: &types.LogNormalRiskModel{
			RiskAversionParameter: l.RiskAversionParameter,
			Tau:                   l.Tau,
			Params:                params,
		},
	}, nil
}

// IntoProto ...
func (s *SimpleRiskModelParamsInput) IntoProto() *types.NewMarketConfiguration_Simple {
	return &types.NewMarketConfiguration_Simple{
		Simple: &types.SimpleModelParams{
			FactorLong:  s.FactorLong,
			FactorShort: s.FactorShort,
		},
	}
}

// IntoProto ...
func (r *RiskParametersInput) IntoProto(target *types.NewMarketConfiguration) error {
	if r.Simple != nil {
		target.RiskParameters = r.Simple.IntoProto()
		return nil
	} else if r.LogNormal != nil {
		var err error
		target.RiskParameters, err = r.LogNormal.IntoProto()
		return err
	}
	return ErrNilRiskModel
}

// TradingModeIntoProto ...
func (n *NewMarketInput) TradingModeIntoProto(target *types.NewMarketConfiguration) error {
	if n.ContinuousTrading != nil && n.DiscreteTrading != nil {
		return ErrAmbiguousTradingMode
	} else if n.ContinuousTrading == nil && n.DiscreteTrading == nil {
		return ErrNilTradingMode
	}

	if n.ContinuousTrading != nil {
		if n.ContinuousTrading.TickSize < 0 {
			return errors.New("ContinuousTrading.TickSize: cannot be < 0")
		}
		target.TradingMode = &types.NewMarketConfiguration_Continuous{
			Continuous: &types.ContinuousTrading{
				TickSize: uint64(n.ContinuousTrading.TickSize),
			},
		}
	} else if n.DiscreteTrading != nil {
		if n.DiscreteTrading.TickSize < 0 {
			return errors.New("DiscreteTrading.TickSize: cannot be < 0")
		}
		if n.DiscreteTrading.Duration <= 0 {
			return errors.New("DiscreteTrading.Duration: cannot be < 0")
		}
		target.TradingMode = &types.NewMarketConfiguration_Discrete{
			Discrete: &types.DiscreteTrading{
				DurationNs: int64(n.DiscreteTrading.Duration),
				TickSize:   uint64(n.DiscreteTrading.TickSize),
			},
		}
	}
	return nil
}

func (b *BuiltinAssetInput) IntoProto() (*types.BuiltinAsset, error) {
	if len(b.Name) <= 0 {
		return nil, errors.New("BuiltinAssetInput.Name: cannot be empty")
	}
	if len(b.Symbol) <= 0 {
		return nil, errors.New("BuiltinAssetInput.Symbol: cannot be empty")
	}
	if len(b.TotalSupply) <= 0 {
		return nil, errors.New("BuiltinAssetInput.Decimals: cannot be empty")
	}
	if b.Decimals <= 0 {
		return nil, errors.New("BuiltinAssetInput.Decimals: cannot be <= 0")
	}

	return &types.BuiltinAsset{
		Name:        b.Name,
		Symbol:      b.Symbol,
		TotalSupply: b.TotalSupply,
		Decimals:    uint64(b.Decimals),
	}, nil
}

func (e *ERC20Input) IntoProto() (*types.ERC20, error) {
	if len(e.ContractAddress) <= 0 {
		return nil, errors.New("ERC20.ContractAddress: cannot be empty")
	}

	return &types.ERC20{
		ContractAddress: e.ContractAddress,
	}, nil
}

func (n *NewAssetInput) IntoProto() (*types.AssetSource, error) {
	var (
		isSet       bool
		assetSource *types.AssetSource = &types.AssetSource{}
	)

	if n.BuiltinAsset != nil {
		isSet = true
		source, err := n.BuiltinAsset.IntoProto()
		if err != nil {
			return nil, err
		}
		assetSource.Source = &types.AssetSource_BuiltinAsset{
			BuiltinAsset: source,
		}
	}

	if n.Erc20 != nil {
		if isSet == true {
			return nil, ErrMultipleAssetSourcesSpecified
		}
		isSet = true
		source, err := n.Erc20.IntoProto()
		if err != nil {
			return nil, err
		}
		assetSource.Source = &types.AssetSource_Erc20{
			Erc20: source,
		}
	}

	return assetSource, nil
}

// IntoProto ...
func (n *NewMarketInput) IntoProto() (*types.NewMarketConfiguration, error) {
	if n.DecimalPlaces < 0 {
		return nil, errors.New("NewMarket.DecimalPlaces: needs to be > 0")
	}
	instrument, err := n.Instrument.IntoProto()
	if err != nil {
		return nil, err
	}

	result := &types.NewMarketConfiguration{
		Instrument:    instrument,
		DecimalPlaces: uint64(n.DecimalPlaces),
	}

	if err := n.RiskParameters.IntoProto(result); err != nil {
		return nil, err
	}
	if err := n.TradingModeIntoProto(result); err != nil {
		return nil, err
	}
	for _, tag := range n.Metadata {
		if tag != nil {
			result.Metadata = append(result.Metadata, *tag)
		}
	}
	return result, nil
}

// IntoProto ...
func (p ProposalTermsInput) IntoProto() (*types.ProposalTerms, error) {
	closing, err := datetimeToSecondsTS(p.ClosingDatetime)
	if err != nil {
		err = fmt.Errorf("ProposalTerms.ClosingDatetime: %s", err.Error())
		return nil, err
	}
	enactment, err := datetimeToSecondsTS(p.EnactmentDatetime)
	if err != nil {
		err = fmt.Errorf("ProposalTerms.EnactementDatetime: %s", err.Error())
		return nil, err
	}

	result := &types.ProposalTerms{
		ClosingTimestamp:   closing,
		EnactmentTimestamp: enactment,
	}

	// used to check if the user did not specify multiple ProposalChanges
	// which is an error
	var isSet bool

	if p.UpdateMarket != nil {
		isSet = true
		result.Change = &types.ProposalTerms_UpdateMarket{}
	}

	if p.NewMarket != nil {
		if isSet {
			return nil, ErrMultipleProposalChangesSpecified
		}
		isSet = true
		market, err := p.NewMarket.IntoProto()
		if err != nil {
			return nil, err
		}
		result.Change = &types.ProposalTerms_NewMarket{
			NewMarket: &types.NewMarket{
				Changes: market,
			},
		}
	}

	if p.NewAsset != nil {
		if isSet {
			return nil, ErrMultipleProposalChangesSpecified
		}
		isSet = true
		assetSource, err := p.NewAsset.IntoProto()
		if err != nil {
			return nil, err
		}
		result.Change = &types.ProposalTerms_NewAsset{
			NewAsset: &types.NewAsset{
				Changes: assetSource,
			},
		}
	}

	if p.UpdateNetwork != nil {
		if isSet {
			return nil, ErrMultipleProposalChangesSpecified
		}
		isSet = true
		result.Change = &types.ProposalTerms_UpdateMarket{}
	}
	if !isSet {
		return nil, ErrInvalidChange
	}

	return result, nil
}

// ToOptionalProposalState ...
func (s *ProposalState) ToOptionalProposalState() (*protoapi.OptionalProposalState, error) {
	if s != nil {
		value, err := s.IntoProtoValue()
		if err != nil {
			return nil, err
		}
		return &protoapi.OptionalProposalState{
			Value: value,
		}, nil
	}
	return nil, nil
}

// IntoProtoValue ...
func (s ProposalState) IntoProtoValue() (types.Proposal_State, error) {
	return convertProposalStateToProto(s)
}

// ProposalVoteFromProto ...
func ProposalVoteFromProto(v *types.Vote, caster *types.Party) *ProposalVote {
	value, _ := convertVoteValueFromProto(v.Value)
	return &ProposalVote{
		Vote: &Vote{
			Party:    caster,
			Value:    value,
			Datetime: nanoTSToDatetime(v.Timestamp),
		},
		ProposalID: v.ProposalID,
	}
}

// IntoProto ...
func (a AccountType) IntoProto() types.AccountType {
	at, _ := convertAccountTypeToProto(a)
	return at
}

func BuiltinAssetFromProto(ba *types.BuiltinAsset) *BuiltinAsset {
	return &BuiltinAsset{
		Name:        ba.Name,
		Symbol:      ba.Symbol,
		TotalSupply: ba.TotalSupply,
		Decimals:    int(ba.Decimals),
	}
}

func ERC20FromProto(ea *types.ERC20) *Erc20 {
	return &Erc20{
		ContractAddress: ea.ContractAddress,
	}
}

func AssetSourceFromProto(psource interface{}) (AssetSource, error) {
	if psource == nil {
		return nil, ErrNilAssetSource
	}
	switch asimpl := psource.(type) {
	case *types.Asset_BuiltinAsset:
		return BuiltinAssetFromProto(asimpl.BuiltinAsset), nil
	case *types.Asset_Erc20:
		return ERC20FromProto(asimpl.Erc20), nil
	default:
		return nil, ErrUnimplementedAssetSource
	}
}

func AssetFromProto(passet *types.Asset) (*Asset, error) {
	source, err := AssetSourceFromProto(passet.Source)
	if err != nil {
		return nil, err
	}

	return &Asset{
		ID:          passet.ID,
		Name:        passet.Name,
		Symbol:      passet.Symbol,
		Decimals:    int(passet.Decimals),
		TotalSupply: passet.TotalSupply,
		Source:      source,
	}, nil
}

func NewAssetFromProto(newAsset *types.NewAsset) (*NewAsset, error) {
	source, err := AssetSourceFromProto(newAsset.Changes)
	if err != nil {
		return nil, err
	}
	return &NewAsset{
		Source: source,
	}, nil
}
