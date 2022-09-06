package api_test

import (
	"context"
	"fmt"
	"testing"

	"code.vegaprotocol.io/vega/libs/jsonrpc"
	vgrand "code.vegaprotocol.io/vega/libs/rand"
	"code.vegaprotocol.io/vega/wallet/api"
	"code.vegaprotocol.io/vega/wallet/api/mocks"
	"code.vegaprotocol.io/vega/wallet/wallet"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdminAnnotateKey(t *testing.T) {
	t.Run("Annotating a key with invalid params fails", testAnnotatingKeyWithInvalidParamsFails)
	t.Run("Annotating a key with valid params succeeds", testAnnotatingKeyWithValidParamsSucceeds)
	t.Run("Annotating a key on unknown wallet fails", testAnnotatingKeyOnUnknownWalletFails)
	t.Run("Annotating a key on unknown key fails", testAnnotatingKeyOnUnknownKeyFails)
	t.Run("Getting internal error during wallet verification doesn't annotate the key", testGettingInternalErrorDuringWalletVerificationDoesNotAnnotateKey)
	t.Run("Getting internal error during wallet retrieval doesn't annotate the key", testGettingInternalErrorDuringWalletRetrievalDoesNotAnnotateKey)
	t.Run("Getting internal error during wallet saving doesn't annotate the key", testGettingInternalErrorDuringWalletSavingDoesNotAnnotateKey)
}

func testAnnotatingKeyWithInvalidParamsFails(t *testing.T) {
	tcs := []struct {
		name          string
		params        interface{}
		expectedError error
	}{
		{
			name:          "with nil params",
			params:        nil,
			expectedError: api.ErrParamsRequired,
		}, {
			name:          "with wrong type of params",
			params:        "test",
			expectedError: api.ErrParamsDoNotMatch,
		}, {
			name: "with empty name",
			params: api.AdminAnnotateKeyParams{
				Wallet:     "",
				PublicKey:  "b5fd9d3c4ad553cb3196303b6e6df7f484cf7f5331a572a45031239fd71ad8a0",
				Metadata:   []wallet.Metadata{{Key: vgrand.RandomStr(5), Value: vgrand.RandomStr(5)}},
				Passphrase: vgrand.RandomStr(5),
			},
			expectedError: api.ErrWalletIsRequired,
		}, {
			name: "with empty passphrase",
			params: api.AdminAnnotateKeyParams{
				PublicKey:  "b5fd9d3c4ad553cb3196303b6e6df7f484cf7f5331a572a45031239fd71ad8a0",
				Metadata:   []wallet.Metadata{{Key: vgrand.RandomStr(5), Value: vgrand.RandomStr(5)}},
				Wallet:     vgrand.RandomStr(5),
				Passphrase: "",
			},
			expectedError: api.ErrPassphraseIsRequired,
		}, {
			name: "with empty public key",
			params: api.AdminAnnotateKeyParams{
				PublicKey:  "",
				Metadata:   []wallet.Metadata{{Key: vgrand.RandomStr(5), Value: vgrand.RandomStr(5)}},
				Wallet:     vgrand.RandomStr(5),
				Passphrase: vgrand.RandomStr(5),
			},
			expectedError: api.ErrPublicKeyIsRequired,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(tt *testing.T) {
			// given
			ctx, _ := contextWithTraceID()

			// setup
			handler := newAnnotateKeyHandler(tt)
			// -- unexpected calls
			handler.walletStore.EXPECT().WalletExists(gomock.Any(), gomock.Any()).Times(0)
			handler.walletStore.EXPECT().ListWallets(gomock.Any()).Times(0)
			handler.walletStore.EXPECT().GetWallet(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			handler.walletStore.EXPECT().SaveWallet(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			handler.walletStore.EXPECT().DeleteWallet(gomock.Any(), gomock.Any()).Times(0)

			// when
			result, errorDetails := handler.handle(t, ctx, tc.params)

			// then
			require.Empty(tt, result)
			assertInvalidParams(tt, errorDetails, tc.expectedError)
		})
	}
}

func testAnnotatingKeyWithValidParamsSucceeds(t *testing.T) {
	// given
	ctx := context.Background()
	passphrase := vgrand.RandomStr(5)
	expectedWallet, kp := walletWithKey(t)

	// setup
	handler := newAnnotateKeyHandler(t)
	// -- expected calls
	handler.walletStore.EXPECT().WalletExists(ctx, expectedWallet.Name()).Times(1).Return(true, nil)
	handler.walletStore.EXPECT().GetWallet(ctx, expectedWallet.Name(), passphrase).Times(1).Return(expectedWallet, nil)
	handler.walletStore.EXPECT().SaveWallet(ctx, expectedWallet, passphrase).Times(1).Return(nil)
	// -- unexpected calls
	handler.walletStore.EXPECT().ListWallets(gomock.Any()).Times(0)
	handler.walletStore.EXPECT().DeleteWallet(gomock.Any(), gomock.Any()).Times(0)

	// when
	result, errorDetails := handler.handle(t, ctx, api.AdminAnnotateKeyParams{
		Wallet:     expectedWallet.Name(),
		Passphrase: passphrase,
		PublicKey:  kp.PublicKey(),
		Metadata:   []wallet.Metadata{{Key: "mode", Value: "test"}},
	})

	// then
	require.Nil(t, errorDetails)
	expectedMeta := []wallet.Metadata{{Key: "mode", Value: "test"}, {Key: "name", Value: "Key 1"}}
	assert.Equal(t, expectedMeta, result.Metadata)
	assert.Equal(t, expectedMeta, expectedWallet.ListKeyPairs()[0].Metadata())
}

func testAnnotatingKeyOnUnknownWalletFails(t *testing.T) {
	// given
	ctx := context.Background()
	passphrase := vgrand.RandomStr(5)
	name := vgrand.RandomStr(5)

	// setup
	handler := newAnnotateKeyHandler(t)
	// -- expected calls
	handler.walletStore.EXPECT().WalletExists(ctx, name).Times(1).Return(false, nil)
	// -- unexpected calls
	handler.walletStore.EXPECT().SaveWallet(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
	handler.walletStore.EXPECT().GetWallet(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
	handler.walletStore.EXPECT().ListWallets(gomock.Any()).Times(0)
	handler.walletStore.EXPECT().DeleteWallet(gomock.Any(), gomock.Any()).Times(0)

	// when
	result, errorDetails := handler.handle(t, ctx, api.AdminAnnotateKeyParams{
		Wallet:     name,
		PublicKey:  vgrand.RandomStr(5),
		Metadata:   []wallet.Metadata{{Key: "mode", Value: "test"}},
		Passphrase: passphrase,
	})

	// then
	require.NotNil(t, errorDetails)
	assert.Empty(t, result)
	assertInvalidParams(t, errorDetails, api.ErrWalletDoesNotExist)
}

func testAnnotatingKeyOnUnknownKeyFails(t *testing.T) {
	// given
	ctx := context.Background()
	passphrase := vgrand.RandomStr(5)
	expectedWallet, _ := walletWithKey(t)

	// setup
	handler := newAnnotateKeyHandler(t)
	// -- expected calls
	handler.walletStore.EXPECT().WalletExists(ctx, expectedWallet.Name()).Times(1).Return(true, nil)
	handler.walletStore.EXPECT().GetWallet(ctx, expectedWallet.Name(), passphrase).Times(1).Return(expectedWallet, nil)
	// -- unexpected calls
	handler.walletStore.EXPECT().SaveWallet(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
	handler.walletStore.EXPECT().ListWallets(gomock.Any()).Times(0)
	handler.walletStore.EXPECT().DeleteWallet(gomock.Any(), gomock.Any()).Times(0)

	// when
	result, errorDetails := handler.handle(t, ctx, api.AdminAnnotateKeyParams{
		Wallet:     expectedWallet.Name(),
		PublicKey:  vgrand.RandomStr(5),
		Metadata:   []wallet.Metadata{{Key: "mode", Value: "test"}},
		Passphrase: passphrase,
	})

	// then
	require.NotNil(t, errorDetails)
	assert.Empty(t, result)
	assertInvalidParams(t, errorDetails, api.ErrPublicKeyDoesNotExist)
}

func testGettingInternalErrorDuringWalletVerificationDoesNotAnnotateKey(t *testing.T) {
	// given
	ctx := context.Background()
	passphrase := vgrand.RandomStr(5)
	expectedWallet, kp := walletWithKey(t)

	// setup
	handler := newAnnotateKeyHandler(t)
	// -- expected calls
	handler.walletStore.EXPECT().WalletExists(ctx, expectedWallet.Name()).Times(1).Return(false, assert.AnError)
	// -- unexpected calls
	handler.walletStore.EXPECT().SaveWallet(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
	handler.walletStore.EXPECT().GetWallet(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
	handler.walletStore.EXPECT().ListWallets(gomock.Any()).Times(0)
	handler.walletStore.EXPECT().DeleteWallet(gomock.Any(), gomock.Any()).Times(0)

	// when
	result, errorDetails := handler.handle(t, ctx, api.AdminAnnotateKeyParams{
		Wallet:     expectedWallet.Name(),
		Passphrase: passphrase,
		PublicKey:  kp.PublicKey(),
		Metadata:   []wallet.Metadata{{Key: "mode", Value: "test"}},
	})

	// then
	require.NotNil(t, errorDetails)
	assert.Empty(t, result)
	assertInternalError(t, errorDetails, fmt.Errorf("could not verify the wallet existence: %w", assert.AnError))
}

func testGettingInternalErrorDuringWalletRetrievalDoesNotAnnotateKey(t *testing.T) {
	// given
	ctx := context.Background()
	passphrase := vgrand.RandomStr(5)
	expectedWallet, kp := walletWithKey(t)

	// setup
	handler := newAnnotateKeyHandler(t)
	// -- expected calls
	handler.walletStore.EXPECT().WalletExists(ctx, expectedWallet.Name()).Times(1).Return(true, nil)
	handler.walletStore.EXPECT().GetWallet(ctx, expectedWallet.Name(), passphrase).Times(1).Return(nil, assert.AnError)
	// -- unexpected calls
	handler.walletStore.EXPECT().SaveWallet(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
	handler.walletStore.EXPECT().ListWallets(gomock.Any()).Times(0)
	handler.walletStore.EXPECT().DeleteWallet(gomock.Any(), gomock.Any()).Times(0)

	// when
	result, errorDetails := handler.handle(t, ctx, api.AdminAnnotateKeyParams{
		Wallet:     expectedWallet.Name(),
		Passphrase: passphrase,
		PublicKey:  kp.PublicKey(),
		Metadata:   []wallet.Metadata{{Key: "mode", Value: "test"}},
	})

	// then
	require.NotNil(t, errorDetails)
	assert.Empty(t, result)
	assertInternalError(t, errorDetails, fmt.Errorf("could not retrieve the wallet: %w", assert.AnError))
}

func testGettingInternalErrorDuringWalletSavingDoesNotAnnotateKey(t *testing.T) {
	// given
	ctx := context.Background()
	passphrase := vgrand.RandomStr(5)
	expectedWallet, kp := walletWithKey(t)

	// setup
	handler := newAnnotateKeyHandler(t)
	// -- expected calls
	handler.walletStore.EXPECT().WalletExists(ctx, expectedWallet.Name()).Times(1).Return(true, nil)
	handler.walletStore.EXPECT().GetWallet(ctx, expectedWallet.Name(), passphrase).Times(1).Return(expectedWallet, nil)
	handler.walletStore.EXPECT().SaveWallet(ctx, gomock.Any(), passphrase).Times(1).Return(assert.AnError)
	// -- unexpected calls
	handler.walletStore.EXPECT().ListWallets(gomock.Any()).Times(0)
	handler.walletStore.EXPECT().DeleteWallet(gomock.Any(), gomock.Any()).Times(0)

	// when
	result, errorDetails := handler.handle(t, ctx, api.AdminAnnotateKeyParams{
		Wallet:     expectedWallet.Name(),
		Passphrase: passphrase,
		PublicKey:  kp.PublicKey(),
		Metadata:   []wallet.Metadata{{Key: "mode", Value: "test"}},
	})

	// then
	require.NotNil(t, errorDetails)
	assert.Empty(t, result)
	assertInternalError(t, errorDetails, fmt.Errorf("could not save the wallet: %w", assert.AnError))
}

type annotateKeyHandler struct {
	*api.AdminAnnotateKey
	ctrl        *gomock.Controller
	walletStore *mocks.MockWalletStore
}

func (h *annotateKeyHandler) handle(t *testing.T, ctx context.Context, params interface{}) (api.AdminAnnotateKeyResult, *jsonrpc.ErrorDetails) {
	t.Helper()

	rawResult, err := h.Handle(ctx, params)
	if rawResult != nil {
		result, ok := rawResult.(api.AdminAnnotateKeyResult)
		if !ok {
			t.Fatal("AdminAnnotateKey handler result is not a AdminAnnotateKeyResult")
		}
		return result, err
	}
	return api.AdminAnnotateKeyResult{}, err
}

func newAnnotateKeyHandler(t *testing.T) *annotateKeyHandler {
	t.Helper()

	ctrl := gomock.NewController(t)
	walletStore := mocks.NewMockWalletStore(ctrl)

	return &annotateKeyHandler{
		AdminAnnotateKey: api.NewAdminAnnotateKey(walletStore),
		ctrl:             ctrl,
		walletStore:      walletStore,
	}
}
