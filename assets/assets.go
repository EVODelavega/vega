package assets

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"code.vegaprotocol.io/vega/assets/builtin"
	"code.vegaprotocol.io/vega/assets/erc20"
	"code.vegaprotocol.io/vega/crypto"
	"code.vegaprotocol.io/vega/logging"
	"code.vegaprotocol.io/vega/nodewallet"
	types "code.vegaprotocol.io/vega/proto"
)

var (
	ErrAssetInvalid      = errors.New("asset invalid")
	ErrAssetDoesNotExist = errors.New("asset does not exist")
	ErrAssetExistForID   = errors.New("an asset already exist for this ID")
	ErrUnknowAssetSource = errors.New("unknown asset source")
	ErrNoAssetForRef     = errors.New("no assets for proposal reference")
)

// TimeService ...
//go:generate go run github.com/golang/mock/mockgen -destination mocks/time_service_mock.go -package mocks code.vegaprotocol.io/vega/assets TimeService
type TimeService interface {
	NotifyOnTick(f func(context.Context, time.Time))
}

type NodeWallet interface {
	Get(chain nodewallet.Blockchain) (nodewallet.Wallet, bool)
}

type Service struct {
	log *logging.Logger
	cfg Config

	// id to asset
	// these assets exists and have been save
	assets map[string]*Asset
	amu    sync.RWMutex

	// this is a list of pending asset which are currently going through
	// proposal, they can later on be promoted to the asset lists once
	// the proposal is accepted by both the nodes and the users
	pendingAssets map[string]*Asset
	apmu          sync.RWMutex

	// map of reference to proposal id
	// use to find back an asset when the governance process
	// is still ongoing
	refs map[string]string
	rmu  sync.RWMutex

	nw NodeWallet
}

func New(log *logging.Logger, cfg Config, nw NodeWallet, ts TimeService) (*Service, error) {
	log = log.Named(namedLogger)
	log.SetLevel(cfg.Level.Get())

	s := &Service{
		log:           log,
		cfg:           cfg,
		assets:        map[string]*Asset{},
		pendingAssets: map[string]*Asset{},
		refs:          map[string]string{},
		nw:            nw,
	}
	ts.NotifyOnTick(s.onTick)
	return s, nil
}

// ReloadConf updates the internal configuration
func (s *Service) ReloadConf(cfg Config) {
	s.log.Info("reloading configuration")
	if s.log.GetLevel() != cfg.Level.Get() {
		s.log.Info("updating log level",
			logging.String("old", s.log.GetLevel().String()),
			logging.String("new", cfg.Level.String()),
		)
		s.log.SetLevel(cfg.Level.Get())
	}

	s.cfg = cfg
}

func (*Service) onTick(_ context.Context, t time.Time) {}

// Enable move the state of an from pending the list of valid and accepted assets
func (s *Service) Enable(assetID string) error {
	s.apmu.Lock()
	defer s.apmu.Unlock()
	asset, ok := s.pendingAssets[assetID]
	if !ok {
		return ErrAssetDoesNotExist
	}
	if asset.IsValid() {
		s.amu.Lock()
		defer s.amu.Unlock()
		s.assets[assetID] = asset
		delete(s.pendingAssets, assetID)
		return nil
	}
	return ErrAssetInvalid
}

func (s *Service) IsEnabled(assetID string) bool {
	s.amu.RLock()
	defer s.amu.RUnlock()
	_, ok := s.assets[assetID]
	return ok
}

// NewAsset add a new asset to the pending list of assets
// the ref is the reference of proposal which submitted the new asset
// returns the assetID and an error
func (s *Service) NewAsset(assetID string, assetSrc *types.AssetSource) (string, error) {
	s.apmu.Lock()
	defer s.apmu.Unlock()
	src := assetSrc.Source
	switch assetSrcImpl := src.(type) {
	case *types.AssetSource_BuiltinAsset:
		s.pendingAssets[assetID] = &Asset{builtin.New(assetID, assetSrcImpl.BuiltinAsset)}
	case *types.AssetSource_Erc20:
		wal, ok := s.nw.Get(nodewallet.Ethereum)
		if !ok {
			return "", errors.New("missing wallet for ETH")
		}
		asset, err := erc20.New(assetID, assetSrcImpl.Erc20, wal)
		if err != nil {
			return "", err
		}
		s.pendingAssets[assetID] = &Asset{asset}
	default:
		return "", ErrUnknowAssetSource
	}

	s.rmu.Lock()
	defer s.rmu.Unlock()
	// setup the ref lookup table
	s.refs[assetID] = assetID

	return assetID, nil
}

// RemovePending remove and asset from the list of pending assets
func (s *Service) RemovePending(assetID string) error {
	s.apmu.Lock()
	defer s.apmu.Unlock()
	_, ok := s.pendingAssets[assetID]
	if !ok {
		return ErrAssetDoesNotExist
	}
	delete(s.pendingAssets, assetID)
	return nil
}

func (s *Service) assetHash(asset *Asset) []byte {
	data := asset.ProtoAsset()
	buf := fmt.Sprintf("%v%v%v%v%v",
		data.ID,
		data.Name,
		data.Symbol,
		data.TotalSupply,
		data.Decimals)
	return crypto.Hash([]byte(buf))
}

func (s *Service) Get(assetID string) (*Asset, error) {
	s.amu.RLock()
	defer s.amu.RUnlock()
	asset, ok := s.assets[assetID]
	if ok {
		return asset, nil
	}
	s.apmu.RLock()
	defer s.apmu.RUnlock()
	asset, ok = s.pendingAssets[assetID]
	if ok {
		return asset, nil
	}
	return nil, ErrAssetDoesNotExist
}

func (s *Service) GetByRef(ref string) (*Asset, error) {
	s.rmu.RLock()
	defer s.rmu.RUnlock()
	id, ok := s.refs[ref]
	if !ok {
		return nil, ErrNoAssetForRef
	}

	return s.Get(id)
}

// AssetHash return an hash of the given asset to be used
// signed to validate the asset on the vega chain
func (s *Service) AssetHash(assetID string) ([]byte, error) {
	asset, err := s.Get(assetID)
	if err != nil {
		return nil, ErrAssetDoesNotExist
	}
	return s.assetHash(asset), nil
}
