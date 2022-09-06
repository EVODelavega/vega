package api

import (
	"context"
	"fmt"

	"code.vegaprotocol.io/vega/libs/jsonrpc"
	"github.com/mitchellh/mapstructure"
)

type AdminTaintKeyParams struct {
	Wallet     string `json:"wallet"`
	PublicKey  string `json:"publicKey"`
	Passphrase string `json:"passphrase"`
}

type AdminTaintKey struct {
	walletStore WalletStore
}

// Handle marks the specified public key as tainted. It makes it unusable for
// transaction signing.
func (h *AdminTaintKey) Handle(ctx context.Context, rawParams jsonrpc.Params) (jsonrpc.Result, *jsonrpc.ErrorDetails) {
	params, err := validateTaintKeyParams(rawParams)
	if err != nil {
		return nil, invalidParams(err)
	}

	if exist, err := h.walletStore.WalletExists(ctx, params.Wallet); err != nil {
		return nil, internalError(fmt.Errorf("could not verify the wallet existence: %w", err))
	} else if !exist {
		return nil, invalidParams(ErrWalletDoesNotExist)
	}

	w, err := h.walletStore.GetWallet(ctx, params.Wallet, params.Passphrase)
	if err != nil {
		return nil, internalError(fmt.Errorf("could not retrieve the wallet: %w", err))
	}

	if !w.HasPublicKey(params.PublicKey) {
		return nil, invalidParams(ErrPublicKeyDoesNotExist)
	}

	if err := w.TaintKey(params.PublicKey); err != nil {
		return nil, internalError(fmt.Errorf("could not taint the key: %w", err))
	}

	if err := h.walletStore.SaveWallet(ctx, w, params.Passphrase); err != nil {
		return nil, internalError(fmt.Errorf("could not save the wallet: %w", err))
	}

	return nil, nil
}

func validateTaintKeyParams(rawParams jsonrpc.Params) (AdminTaintKeyParams, error) {
	if rawParams == nil {
		return AdminTaintKeyParams{}, ErrParamsRequired
	}

	params := AdminTaintKeyParams{}
	if err := mapstructure.Decode(rawParams, &params); err != nil {
		return AdminTaintKeyParams{}, ErrParamsDoNotMatch
	}

	if params.Wallet == "" {
		return AdminTaintKeyParams{}, ErrWalletIsRequired
	}

	if params.Passphrase == "" {
		return AdminTaintKeyParams{}, ErrPassphraseIsRequired
	}

	if params.PublicKey == "" {
		return AdminTaintKeyParams{}, ErrPublicKeyIsRequired
	}

	return params, nil
}

func NewAdminTaintKey(
	walletStore WalletStore,
) *AdminTaintKey {
	return &AdminTaintKey{
		walletStore: walletStore,
	}
}
