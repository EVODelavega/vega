package evtforward_test

import (
	"testing"
	"time"

	"code.vegaprotocol.io/vega/evtforward"
	"code.vegaprotocol.io/vega/evtforward/mocks"
	"code.vegaprotocol.io/vega/logging"
	types "code.vegaprotocol.io/vega/proto"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

var (
	testSelfVegaPubKey = []byte("self-pubkey")
	testAllPubKeys     = [][]byte{
		testSelfVegaPubKey,
		[]byte("another-pubkey1"),
		[]byte("another-pubkey2"),
	}
	initTime = time.Unix(10, 0)
)

type testEvtFwd struct {
	*evtforward.EvtForwarder
	ctrl *gomock.Controller
	time *mocks.MockTimeService
	top  *mocks.MockValidatorTopology
	cmd  *mocks.MockCommander
	cb   func(t time.Time)
}

func getTestEvtFwd(t *testing.T) *testEvtFwd {
	ctrl := gomock.NewController(t)
	tim := mocks.NewMockTimeService(ctrl)
	top := mocks.NewMockValidatorTopology(ctrl)
	cmd := mocks.NewMockCommander(ctrl)

	top.EXPECT().AllPubKeys().Times(1).Return(testAllPubKeys)
	top.EXPECT().SelfVegaPubKey().Times(1).Return(testSelfVegaPubKey)
	var cb func(time.Time)
	tim.EXPECT().NotifyOnTick(gomock.Any()).Do(func(f func(t time.Time)) {
		cb = f
	})

	tim.EXPECT().GetTimeNow().Times(1).Return(initTime, nil)

	evtfwd, err := evtforward.New(
		logging.NewTestLogger(), evtforward.NewDefaultConfig(),
		cmd, tim, top)
	assert.NoError(t, err)

	return &testEvtFwd{
		EvtForwarder: evtfwd,
		ctrl:         ctrl,
		time:         tim,
		top:          top,
		cmd:          cmd,
		cb:           cb,
	}
}

func TestEvtForwarder(t *testing.T) {
	t.Run("test forward success node is forwarder", testForwardSuccessNodeIsForwarder)
	t.Run("test forward failure duplicate event", testForwardFailureDuplicateEvent)
	t.Run("test ensure validators lists are updated", testUpdateValidatorList)
	t.Run("test ack success", testAckSuccess)
	t.Run("test ack failure already acked", testAckFailureAlreadyAcked)
}

func testForwardSuccessNodeIsForwarder(t *testing.T) {
	evtfwd := getTestEvtFwd(t)
	defer evtfwd.ctrl.Finish()
	evt := getTestChainEvent()
	evtfwd.cmd.EXPECT().Command(gomock.Any(), gomock.Any()).Return(nil)
	evtfwd.top.EXPECT().AllPubKeys().Times(1).Return(testAllPubKeys)
	// set the time so the hash match our current node
	evtfwd.cb(time.Unix(11, 0))
	err := evtfwd.Forward(evt)
	assert.NoError(t, err)
}

func testForwardFailureDuplicateEvent(t *testing.T) {
	evtfwd := getTestEvtFwd(t)
	defer evtfwd.ctrl.Finish()
	evt := getTestChainEvent()
	evtfwd.cmd.EXPECT().Command(gomock.Any(), gomock.Any()).Return(nil)
	evtfwd.top.EXPECT().AllPubKeys().Times(1).Return(testAllPubKeys)
	// set the time so the hash match our current node
	evtfwd.cb(time.Unix(11, 0))
	err := evtfwd.Forward(evt)
	assert.NoError(t, err)

	// now the event should exist, let's try toforward againt
	err = evtfwd.Forward(evt)
	assert.EqualError(t, err, evtforward.ErrEvtAlreadyExist.Error())
}

func testUpdateValidatorList(t *testing.T) {
	evtfwd := getTestEvtFwd(t)
	defer evtfwd.ctrl.Finish()
	// no event, just call callback to ensure the validator list is updated
	evtfwd.top.EXPECT().AllPubKeys().Times(1).Return(testAllPubKeys)
	evtfwd.cb(initTime.Add(time.Second))
}

func testAckSuccess(t *testing.T) {
	evtfwd := getTestEvtFwd(t)
	defer evtfwd.ctrl.Finish()
	evt := getTestChainEvent()
	ok := evtfwd.Ack(evt)
	assert.True(t, ok)
}

func testAckFailureAlreadyAcked(t *testing.T) {
	evtfwd := getTestEvtFwd(t)
	defer evtfwd.ctrl.Finish()
	evt := getTestChainEvent()
	ok := evtfwd.Ack(evt)
	assert.True(t, ok)
	// try to ack again
	ko := evtfwd.Ack(evt)
	assert.False(t, ko)
}

func getTestChainEvent() *types.ChainEvent {
	return &types.ChainEvent{
		TxID: "somehash",
		Event: &types.ChainEvent_Erc20{
			Erc20: &types.ERC20Event{
				Index: 1,
				Block: 100,
				Action: &types.ERC20Event_AssetList{
					AssetList: &types.ERC20AssetList{
						VegaAssetID: "asset-id-1",
					},
				},
			},
		},
	}
}
