// Copyright (c) 2022 Gobalsky Labs Limited
//
// Use of this software is governed by the Business Source License included
// in the LICENSE.DATANODE file and at https://www.mariadb.com/bsl11.
//
// Change Date: 18 months from the later of the date of the first publicly
// available Distribution of this version of the repository, and 25 June 2022.
//
// On the date above, in accordance with the Business Source License, use
// of this software will be governed by version 3 or later of the GNU General
// Public License.

package gql

import (
	"context"
	"strconv"

	"code.vegaprotocol.io/vega/datanode/utils"
	v2 "code.vegaprotocol.io/vega/protos/data-node/api/v2"
	vegapb "code.vegaprotocol.io/vega/protos/vega"
	v1 "code.vegaprotocol.io/vega/protos/vega/data/v1"
)

type oracleSpecResolver VegaResolverRoot

func (o *oracleSpecResolver) DataSourceSpec(_ context.Context, obj *vegapb.OracleSpec) (extDss *ExternalDataSourceSpec, _ error) {
	extDss = &ExternalDataSourceSpec{Spec: &DataSourceSpec{Data: &DataSourceDefinition{}}}
	if obj.ExternalDataSourceSpec != nil {
		extDss.Spec = resolveDataSourceSpec(obj.ExternalDataSourceSpec.Spec)
	}
	return
}

func (o *oracleSpecResolver) DataConnection(ctx context.Context, obj *vegapb.OracleSpec, pagination *v2.Pagination) (*v2.OracleDataConnection, error) {
	var specID *string
	if ed := obj.ExternalDataSourceSpec; ed != nil && ed.Spec != nil && ed.Spec.Id != "" {
		specID = &ed.Spec.Id
	}

	req := v2.ListOracleDataRequest{
		OracleSpecId: specID,
		Pagination:   pagination,
	}

	resp, err := o.tradingDataClientV2.ListOracleData(ctx, &req)
	if err != nil {
		return nil, err
	}

	return resp.OracleData, nil
}

type oracleDataResolver VegaResolverRoot

func (o *oracleDataResolver) ExternalData(_ context.Context, obj *vegapb.OracleData) (ed *ExternalData, _ error) {
	ed = &ExternalData{
		Data: &Data{},
	}

	oed := obj.ExternalData
	if oed == nil || oed.Data == nil {
		return
	}

	ed.Data.Signers = resolveSigners(oed.Data.Signers)
	ed.Data.Data = oed.Data.Data
	ed.Data.MatchedSpecIds = oed.Data.MatchedSpecIds
	ed.Data.BroadcastAt = strconv.FormatInt(oed.Data.BroadcastAt, 10)

	return
}

func resolveSigners(obj []*v1.Signer) (signers []*Signer) {
	for i := range obj {
		signers = append(signers, &Signer{Signer: resolveSigner(obj[i].Signer)})
	}
	return
}

func resolveSigner(obj any) (signer SignerKind) {
	switch sig := obj.(type) {
	case *v1.Signer_PubKey:
		signer = &PubKey{Key: &sig.PubKey.Key}
	case *v1.Signer_EthAddress:
		signer = &ETHAddress{Address: &sig.EthAddress.Address}
	}
	return
}

func resolveDataSourceDefinition(d *vegapb.DataSourceDefinition) (ds *DataSourceDefinition) {
	ds = &DataSourceDefinition{}
	if d == nil {
		return
	}
	switch dst := d.SourceType.(type) {
	case *vegapb.DataSourceDefinition_External:
		ds.SourceType = dst.External
	case *vegapb.DataSourceDefinition_Internal:
		ds.SourceType = dst.Internal
	}
	return
}

func resolveDataSourceSpec(d *vegapb.DataSourceSpec) (ds *DataSourceSpec) {
	ds = &DataSourceSpec{
		Data: &DataSourceDefinition{},
	}
	if d == nil {
		return
	}

	ds.ID = d.GetId()
	ds.CreatedAt = strconv.FormatInt(d.CreatedAt, 10)
	ds.UpdatedAt = utils.ToPtr(strconv.FormatInt(d.UpdatedAt, 10))
	ds.Status = DataSourceSpecStatus(strconv.FormatInt(int64(d.Status), 10))

	if d.Data != nil {
		ds.Data = resolveDataSourceDefinition(d.Data)
	}

	return
}
