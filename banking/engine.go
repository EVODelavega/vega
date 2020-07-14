package banking

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"code.vegaprotocol.io/vega/assets"
	"code.vegaprotocol.io/vega/logging"
	types "code.vegaprotocol.io/vega/proto"
	"code.vegaprotocol.io/vega/validators"

	"golang.org/x/crypto/sha3"
)

//go:generate go run github.com/golang/mock/mockgen -destination mocks/assets_mock.go -package mocks code.vegaprotocol.io/vega/banking Assets
type Assets interface {
	Get(assetID string) (assets.Asset, error)
}

// Collateral ...
//go:generate go run github.com/golang/mock/mockgen -destination mocks/collateral_mock.go -package mocks code.vegaprotocol.io/vega/banking Collateral
type Collateral interface {
	Deposit(ctx context.Context, partyID, asset string, amount uint64) error
	Withdraw(ctx context.Context, partyID, asset string, amount uint64) error
}

// ExtResChecker ...
//go:generate go run github.com/golang/mock/mockgen -destination mocks/ext_res_checker_mock.go -package mocks code.vegaprotocol.io/vega/banking ExtResChecker
type ExtResChecker interface {
	StartCheck(validators.Resource, func(interface{}, bool), time.Time) error
}

// TimeService ...
//go:generate go run github.com/golang/mock/mockgen -destination mocks/time_service_mock.go -package mocks code.vegaprotocol.io/vega/banking TimeService
type TimeService interface {
	GetTimeNow() (time.Time, error)
	NotifyOnTick(func(time.Time))
}

const (
	pendingState uint32 = iota
	okState
	rejectedState
)

var (
	defaultValidationDuration = 2 * time.Hour
)

type Engine struct {
	log       *logging.Logger
	col       Collateral
	erc       ExtResChecker
	assets    Assets
	assetActs map[string]*assetAction
	tsvc      TimeService
}

func New(log *logging.Logger, col Collateral, erc ExtResChecker, tsvc TimeService) (e *Engine) {
	defer func() { tsvc.NotifyOnTick(e.OnTick) }()
	return &Engine{
		log:       log,
		col:       col,
		erc:       erc,
		assetActs: map[string]*assetAction{},
		tsvc:      tsvc,
	}
}

func (e *Engine) onCheckDone(i interface{}, valid bool) {
	aa, ok := i.(*assetAction)
	if !ok {
		return
	}

	var newState = rejectedState
	if valid {
		newState = okState
	}
	atomic.StoreUint32(&aa.state, newState)
}

func (e *Engine) DepositBuiltinAsset(d *types.BuiltinAssetDeposit) error {
	now, _ := e.tsvc.GetTimeNow()
	aa := &assetAction{
		id:       id(d, now),
		state:    pendingState,
		builtinD: d,
	}
	e.assetActs[aa.id] = aa
	return e.erc.StartCheck(aa, e.onCheckDone, now.Add(defaultValidationDuration))
}

func (e *Engine) DepositERC20(d *types.ERC20Deposit) error {
	now, _ := e.tsvc.GetTimeNow()
	aa := &assetAction{
		id:     id(d, now),
		state:  pendingState,
		erc20D: d,
	}
	e.assetActs[aa.id] = aa
	return e.erc.StartCheck(aa, e.onCheckDone, now.Add(defaultValidationDuration))
}

func (e *Engine) OnTick(t time.Time) {
	ctx := context.Background()
	for k, v := range e.assetActs {
		state := atomic.LoadUint32(&v.state)
		if state == pendingState {
			continue
		}
		switch state {
		case okState:
			if err := e.finalizeAction(ctx, v); err != nil {
				e.log.Error("unable to finalize action",
					logging.String("action", v.String()),
					logging.Error(err))
			}
		case rejectedState:
			e.log.Error("network rejected banking action",
				logging.String("action", v.String()))
		}
		// delete anyway
		delete(e.assetActs, k)
	}
}

func (e *Engine) finalizeAction(ctx context.Context, aa *assetAction) error {
	switch {
	case aa.IsBuiltinAssetDeposit():
		d := aa.BuiltinAssetDesposit()
		return e.finalizeDeposit(ctx, d.PartyID, d.VegaAssetID, d.Amount)
	case aa.IsERC20Deposit():
		d := aa.ERC20Deposit()
		_ = d
		return e.finalizeDeposit(ctx, "", "", 0)
	default:
		return ErrUnknownAssetAction
	}
}

func (e *Engine) finalizeDeposit(ctx context.Context, party, asset string, amount uint64) error {
	return e.col.Deposit(ctx, party, asset, amount)
}

type HasVegaAssetID interface {
	GetVegaAssetID() string
}

type Stringer interface {
	String() string
}

func id(s Stringer, now time.Time) string {
	hasher := sha3.New256()
	hasher.Write([]byte(fmt.Sprintf("%v%v", s.String(), now.UnixNano())))
	return string(hasher.Sum(nil))
}
