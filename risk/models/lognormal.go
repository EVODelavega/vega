package models

import (
	"time"

	pd "code.vegaprotocol.io/quant/pricedistribution"
	"code.vegaprotocol.io/quant/riskmodelbs"
	types "code.vegaprotocol.io/vega/proto"
)

// LogNormal represent a future risk model
type LogNormal struct {
	riskAversionParameter, tau float64
	params                     riskmodelbs.ModelParamsBS
	asset                      string
}

// NewBuiltinFutures instantiate a new builtin future
func NewBuiltinFutures(pf *types.LogNormalRiskModel, asset string) (*LogNormal, error) {
	return &LogNormal{
		riskAversionParameter: pf.RiskAversionParameter,
		tau:                   pf.Tau,
		params: riskmodelbs.ModelParamsBS{
			Mu:    pf.Params.Mu,
			R:     pf.Params.R,
			Sigma: pf.Params.Sigma,
		},
		asset: asset,
	}, nil
}

// CalculationInterval return the calculation interval for
// the Forward risk model
func (f *LogNormal) CalculationInterval() time.Duration {
	return time.Duration(0)
}

// CalculateRiskFactors calls the risk model in order to get
// the new risk models
func (f *LogNormal) CalculateRiskFactors(
	current *types.RiskResult) (bool, *types.RiskResult) {
	rawrf := riskmodelbs.RiskFactorsForward(f.riskAversionParameter, f.tau, f.params)
	rf := &types.RiskResult{
		RiskFactors: map[string]*types.RiskFactor{
			f.asset: {
				Long:  rawrf.Long,
				Short: rawrf.Short,
			},
		},
		PredictedNextRiskFactors: map[string]*types.RiskFactor{
			f.asset: {
				Long:  rawrf.Long,
				Short: rawrf.Short,
			},
		},
	}
	return true, rf
}

// PriceRange returns the minimum and maximum price as implied by the model's probability distribution with horizon given by yearFraction (e.g. 0.5 for half a year) and probability level (e.g. 0.95 for 95%).
func (f *LogNormal) PriceRange(currentPrice float64, yearFraction float64, probabilityLevel float32) (minPrice float64, maxPrice float64) {
	dist := f.params.GetProbabilityDistribution(currentPrice, yearFraction)
	minPrice, maxPrice = pd.PriceRange(dist, probabilityLevel)
	return
}
