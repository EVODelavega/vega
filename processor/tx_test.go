package processor_test

import (
	"bytes"
	"testing"

	"code.vegaprotocol.io/vega/processor"
	"code.vegaprotocol.io/vega/types"
	"code.vegaprotocol.io/vega/txn"

	"github.com/golang/protobuf/proto"
	"github.com/stretchr/testify/require"
)

func concatBytes(bzs ...[]byte) []byte {
	buf := bytes.NewBuffer(nil)
	for _, bz := range bzs {
		_, err := buf.Write(bz)
		if err != nil {
			panic(err)
		}
	}
	return buf.Bytes()
}

func txEncode(t *testing.T, cmd txn.Command, msg proto.Message) *types.Transaction {
	payload, err := proto.Marshal(msg)
	require.NoError(t, err)

	bz, err := txn.Encode(payload, cmd)
	require.NoError(t, err)

	return &types.Transaction{
		InputData: bz,
	}
}

type TxTestSuite struct {
}

func (s *TxTestSuite) testValidateSignedInvalidPayload(t *testing.T) {
	t.Run("TooShort", func(t *testing.T) {
		_, err := processor.NewTx(
			&types.Transaction{
				InputData: []byte("shorter-than-37-bytes"),
			},
			[]byte{},
		)
		require.Error(t, err)
	})

	t.Run("RandomCrap", func(t *testing.T) {
		var hash [processor.TxHashLen]byte
		tx, err := processor.NewTx(
			&types.Transaction{
				InputData: concatBytes(
					hash[:],
					[]byte{byte(txn.SubmitOrderCommand)},
					[]byte("foobar"),
				),
			},
			[]byte{},
		)
		require.NoError(t, err)
		require.Error(t, tx.Validate())
	})
}

func TestTxValidation(t *testing.T) {
	s := &TxTestSuite{}

	t.Run("Test validate signed invalid payload", s.testValidateSignedInvalidPayload)
}
