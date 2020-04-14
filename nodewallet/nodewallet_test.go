package nodewallet_test

import (
	"crypto/rand"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"code.vegaprotocol.io/vega/config/encoding"
	"code.vegaprotocol.io/vega/logging"
	"code.vegaprotocol.io/vega/nodewallet"
	"code.vegaprotocol.io/vega/nodewallet/eth"
	"github.com/stretchr/testify/assert"
)

var (
	rootDirPath = "/tmp/vegatests/nodewallet/"
)

func rootDir() string {
	path := filepath.Join(rootDirPath, randSeq(10))
	os.MkdirAll(path, os.ModePerm)
	return path
}

func TestNodeWallet(t *testing.T) {
	t.Run("is supported fail", testIsSupportedFail)
	t.Run("is supported success", testIsSupportedSuccess)
	t.Run("test init success as new node wallet", testInitSuccess)
	t.Run("test init failure as new node wallet", testInitFailure)
	t.Run("test devInit success", testDevInitSuccess)
	t.Run("verify success", testVerifySuccess)
	t.Run("verify failure", testVerifyFailure)
	t.Run("new failure invalid store path", testNewFailureInvalidStorePath)
	t.Run("new failure missing required wallets", testNewFailureMissingRequiredWallets)
	t.Run("new failure invalidPassphrase", testNewFailureInvalidPassphrase)
	t.Run("import new wallet", testImportNewWallet)
}

func testIsSupportedFail(t *testing.T) {
	err := nodewallet.IsSupported("yolocoin")
	assert.EqualError(t, err, "unsupported chain wallet yolocoin")
}

func testIsSupportedSuccess(t *testing.T) {
	err := nodewallet.IsSupported("vega")
	assert.NoError(t, err)
}

func testInitSuccess(t *testing.T) {
	rootDir := rootDir()
	filepath := filepath.Join(rootDir, "nodewalletstore")

	err := nodewallet.Init(filepath, "somepassphrase")
	assert.NoError(t, err)

	assert.NoError(t, os.RemoveAll(rootDir))
}

func testInitFailure(t *testing.T) {
	filepath := filepath.Join("/invalid/path/", "nodewalletstore")

	err := nodewallet.Init(filepath, "somepassphrase")
	assert.EqualError(t, err, "open /invalid/path/nodewalletstore: no such file or directory")
}

func testDevInitSuccess(t *testing.T) {
	rootDir := rootDir()
	filepath := filepath.Join(rootDir, "nodewalletstore")

	// no error to generate
	err := nodewallet.DevInit(filepath, rootDir, "somepassphrase")
	assert.NoError(t, err)

	// try to instanciate a wallet from that
	cfg := nodewallet.Config{
		Level:          encoding.LogLevel{},
		StorePath:      filepath,
		DevWalletsPath: rootDir,
	}
	nw, err := nodewallet.New(logging.NewTestLogger(), cfg, "somepassphrase")
	assert.NoError(t, err)

	// try to get the vega and eth wallet
	w, ok := nw.Get(nodewallet.Eth)
	assert.NotNil(t, w)
	assert.True(t, ok)
	assert.Equal(t, string(nodewallet.Eth), w.Chain())
	w1, ok := nw.Get(nodewallet.Vega)
	assert.NotNil(t, w1)
	assert.True(t, ok)
	assert.Equal(t, string(nodewallet.Vega), w1.Chain())

	assert.NoError(t, os.RemoveAll(rootDir))
}

func testVerifySuccess(t *testing.T) {
	rootDir := rootDir()
	filepath := filepath.Join(rootDir, "nodewalletstore")

	// no error to generate
	err := nodewallet.DevInit(filepath, rootDir, "somepassphrase")
	assert.NoError(t, err)

	// try to instanciate a wallet from that
	cfg := nodewallet.Config{
		Level:          encoding.LogLevel{},
		StorePath:      filepath,
		DevWalletsPath: rootDir,
	}

	err = nodewallet.Verify(cfg, "somepassphrase")
	assert.NoError(t, err)
	assert.NoError(t, os.RemoveAll(rootDir))
}

func testVerifyFailure(t *testing.T) {
	// create a random non existing path
	filepath := filepath.Join("/", randSeq(10), "somewallet")
	cfg := nodewallet.Config{
		Level:          encoding.LogLevel{},
		StorePath:      filepath,
		DevWalletsPath: "",
	}

	err := nodewallet.Verify(cfg, "somepassphrase")
	assert.Error(t, err)
}

func testNewFailureInvalidStorePath(t *testing.T) {
	// create a random non existing path
	filepath := filepath.Join("/", randSeq(10), "somewallet")
	cfg := nodewallet.Config{
		Level:          encoding.LogLevel{},
		StorePath:      filepath,
		DevWalletsPath: "",
	}

	nw, err := nodewallet.New(logging.NewTestLogger(), cfg, "somepassphrase")
	assert.Error(t, err)
	assert.Nil(t, nw)
}

func testNewFailureMissingRequiredWallets(t *testing.T) {
	rootDir := rootDir()
	filepath := filepath.Join(rootDir, "nodewalletstore")

	// no error to generate
	err := nodewallet.Init(filepath, "somepassphrase")
	assert.NoError(t, err)

	// try to instanciate a wallet from that
	cfg := nodewallet.Config{
		Level:          encoding.LogLevel{},
		StorePath:      filepath,
		DevWalletsPath: rootDir,
	}

	nw, err := nodewallet.New(logging.NewTestLogger(), cfg, "somepassphrase")
	assert.EqualError(t, err, "missing required wallet for vega chain")
	assert.Nil(t, nw)
	assert.NoError(t, os.RemoveAll(rootDir))

}

func testImportNewWallet(t *testing.T) {
	ethDir := rootDir()
	rootDir := rootDir()
	filepath := filepath.Join(rootDir, "nodewalletstore")

	// no error to generate
	err := nodewallet.DevInit(filepath, rootDir, "somepassphrase")
	assert.NoError(t, err)

	// try to instanciate a wallet from that
	cfg := nodewallet.Config{
		Level:          encoding.LogLevel{},
		StorePath:      filepath,
		DevWalletsPath: rootDir,
	}

	nw, err := nodewallet.New(logging.NewTestLogger(), cfg, "somepassphrase")
	assert.NoError(t, err)
	assert.NotNil(t, nw)

	// now generate an eth wallet
	path, err := eth.DevInit(ethDir, "ethpassphrase")
	assert.NoError(t, err)
	assert.NotEmpty(t, path)

	// import this new wallet
	err = nw.Import(string(nodewallet.Eth), "somepassphrase", "ethpassphrase", path)
	assert.NoError(t, err)

	assert.NoError(t, os.RemoveAll(rootDir))
	assert.NoError(t, os.RemoveAll(ethDir))
}
func testNewFailureInvalidPassphrase(t *testing.T) {
	rootDir := rootDir()
	filepath := filepath.Join(rootDir, "nodewalletstore")

	// no error to generate
	err := nodewallet.Init(filepath, "somepassphrase")
	assert.NoError(t, err)

	// try to instanciate a wallet from that
	cfg := nodewallet.Config{
		Level:          encoding.LogLevel{},
		StorePath:      filepath,
		DevWalletsPath: rootDir,
	}

	nw, err := nodewallet.New(logging.NewTestLogger(), cfg, "notthesamepassphrase")
	assert.EqualError(t, err, "unable to load nodewalletsore: unable to decrypt store file (cipher: message authentication failed)")
	assert.Nil(t, nw)
	assert.NoError(t, os.RemoveAll(rootDir))
}

var chars = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

func randSeq(n int) string {
	b := make([]rune, n)
	for i := range b {
		v, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		b[i] = chars[v.Int64()]
	}
	return string(b)
}
