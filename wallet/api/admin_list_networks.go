package api

import (
	"context"
	"fmt"

	"code.vegaprotocol.io/vega/libs/jsonrpc"
)

type AdminListNetworksResult struct {
	Networks []string `json:"networks"`
}

type AdminListNetworks struct {
	networkStore NetworkStore
}

// Handle List all registered networks.
func (h *AdminListNetworks) Handle(ctx context.Context, _ jsonrpc.Params) (jsonrpc.Result, *jsonrpc.ErrorDetails) {
	networks, err := h.networkStore.ListNetworks()
	if err != nil {
		return nil, internalError(fmt.Errorf("could not list the networks: %w", err))
	}
	return AdminListNetworksResult{
		Networks: networks,
	}, nil
}

func NewAdminListNetworks(
	networkStore NetworkStore,
) *AdminListNetworks {
	return &AdminListNetworks{
		networkStore: networkStore,
	}
}
