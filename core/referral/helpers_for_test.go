// Copyright (C) 2023 Gobalsky Labs Limited
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package referral_test

import (
	"context"
	"testing"
	"time"

	"code.vegaprotocol.io/vega/core/events"
	"code.vegaprotocol.io/vega/core/integration/stubs"
	"code.vegaprotocol.io/vega/core/referral"
	"code.vegaprotocol.io/vega/core/referral/mocks"
	"code.vegaprotocol.io/vega/core/snapshot"
	"code.vegaprotocol.io/vega/core/stats"
	"code.vegaprotocol.io/vega/core/types"
	vgcrypto "code.vegaprotocol.io/vega/libs/crypto"
	vgrand "code.vegaprotocol.io/vega/libs/rand"
	"code.vegaprotocol.io/vega/logging"
	"code.vegaprotocol.io/vega/paths"
	vegapb "code.vegaprotocol.io/vega/protos/vega"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testEngine struct {
	engine                *referral.SnapshottedEngine
	broker                *mocks.MockBroker
	timeSvc               *mocks.MockTimeService
	marketActivityTracker *mocks.MockMarketActivityTracker
	staking               *mocks.MockStakingBalances
	currentEpoch          uint64
}

func newPartyID(t *testing.T) types.PartyID {
	t.Helper()

	return types.PartyID(vgrand.RandomStr(5))
}

func newSetID(t *testing.T) types.ReferralSetID {
	t.Helper()

	return types.ReferralSetID(vgcrypto.RandomHash())
}

func newSnapshotEngine(t *testing.T, vegaPath paths.Paths, now time.Time, engine *referral.SnapshottedEngine) *snapshot.Engine {
	t.Helper()

	log := logging.NewTestLogger()
	timeService := stubs.NewTimeStub()
	timeService.SetTime(now)
	statsData := stats.New(log, stats.NewDefaultConfig())
	config := snapshot.DefaultConfig()

	snapshotEngine, err := snapshot.NewEngine(vegaPath, config, log, timeService, statsData.Blockchain)
	require.NoError(t, err)

	snapshotEngine.AddProviders(engine)

	return snapshotEngine
}

func newEngine(t *testing.T) *testEngine {
	t.Helper()

	ctrl := gomock.NewController(t)

	broker := mocks.NewMockBroker(ctrl)
	timeSvc := mocks.NewMockTimeService(ctrl)
	mat := mocks.NewMockMarketActivityTracker(ctrl)
	staking := mocks.NewMockStakingBalances(ctrl)

	engine := referral.NewSnapshottedEngine(broker, timeSvc, mat, staking)

	engine.OnEpochRestore(context.Background(), types.Epoch{
		Seq:    10,
		Action: vegapb.EpochAction_EPOCH_ACTION_START,
	})

	return &testEngine{
		engine:                engine,
		broker:                broker,
		timeSvc:               timeSvc,
		marketActivityTracker: mat,
		currentEpoch:          10,
		staking:               staking,
	}
}

func nextEpoch(t *testing.T, ctx context.Context, te *testEngine, startEpochTime time.Time) {
	t.Helper()

	te.engine.OnEpoch(ctx, types.Epoch{
		Seq:     te.currentEpoch,
		Action:  vegapb.EpochAction_EPOCH_ACTION_END,
		EndTime: startEpochTime.Add(-1 * time.Second),
	})

	te.currentEpoch += 1

	te.engine.OnEpoch(ctx, types.Epoch{
		Seq:       te.currentEpoch,
		Action:    vegapb.EpochAction_EPOCH_ACTION_START,
		StartTime: startEpochTime,
	})
}

func expectReferralProgramStartedEvent(t *testing.T, engine *testEngine) {
	t.Helper()

	engine.broker.EXPECT().Send(gomock.Any()).Do(func(evt events.Event) {
		_, ok := evt.(*events.ReferralProgramStarted)
		assert.True(t, ok, "Event should be a ReferralProgramStarted, but is %T", evt)
	}).Times(1)
}

func expectReferralProgramEndedEvent(t *testing.T, engine *testEngine) *gomock.Call {
	t.Helper()

	return engine.broker.EXPECT().Send(gomock.Any()).Do(func(evt events.Event) {
		_, ok := evt.(*events.ReferralProgramEnded)
		assert.True(t, ok, "Event should be a ReferralProgramEnded, but is %T", evt)
	}).Times(1)
}

func expectReferralProgramUpdatedEvent(t *testing.T, engine *testEngine) *gomock.Call {
	t.Helper()

	return engine.broker.EXPECT().Send(gomock.Any()).Do(func(evt events.Event) {
		_, ok := evt.(*events.ReferralProgramUpdated)
		assert.True(t, ok, "Event should be a ReferralProgramUpdated, but is %T", evt)
	}).Times(1)
}

func expectReferralSetStatsUpdatedEvent(t *testing.T, engine *testEngine, times int) *gomock.Call {
	t.Helper()

	return engine.broker.EXPECT().Send(gomock.Any()).Do(func(evt events.Event) {
		_, ok := evt.(*events.ReferralSetStatsUpdated)
		assert.True(t, ok, "Event should be a ReferralSetStatsUpdated, but is %T", evt)
	}).Times(times)
}
