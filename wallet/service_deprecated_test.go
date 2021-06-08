package wallet_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"code.vegaprotocol.io/vega/wallet"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func TestDeprecatedService(t *testing.T) {
	t.Run("sign ok", testServiceSignOK)
	t.Run("sign any", testServiceSignAnyOK)
	t.Run("sign fail invalid request", testServiceSignFailInvalidRequest)
}


func testServiceSignOK(t *testing.T) {
	s := getTestService(t)
	defer s.ctrl.Finish()

	s.handler.EXPECT().SignTx(gomock.Any(), gomock.Any(), gomock.Any()).
		Times(1).Return(wallet.SignedBundle{}, nil)
	payload := `{"tx": "some data", "pubKey": "asdasasdasd"}`
	r := httptest.NewRequest("POST", "scheme://host/path", bytes.NewBufferString(payload))
	r.Header.Set("Authorization", "Bearer eyXXzA")

	w := httptest.NewRecorder()

	wallet.ExtractToken(s.SignTx)(w, r, nil)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func testServiceSignAnyOK(t *testing.T) {
	s := getTestService(t)
	defer s.ctrl.Finish()

	s.handler.EXPECT().SignAny(gomock.Any(), gomock.Any(), gomock.Any()).
		Times(1).Return([]byte("some sig"), nil)
	payload := `{"inputData": "some data", "pubKey": "asdasasdasd"}`
	r := httptest.NewRequest("POST", "scheme://host/path", bytes.NewBufferString(payload))
	r.Header.Set("Authorization", "Bearer eyXXzA")

	w := httptest.NewRecorder()

	wallet.ExtractToken(s.SignAny)(w, r, nil)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func testServiceSignFailInvalidRequest(t *testing.T) {
	s := getTestService(t)
	defer s.ctrl.Finish()

	// InvalidMethod
	r := httptest.NewRequest("GET", "scheme://host/path", nil)
	w := httptest.NewRecorder()

	wallet.ExtractToken(s.SignTx)(w, r, nil)

	resp := w.Result()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// invalid token
	r = httptest.NewRequest("POST", "scheme://host/path", nil)
	r.Header.Set("Authorization", "Bearer")

	w = httptest.NewRecorder()

	wallet.ExtractToken(s.SignTx)(w, r, nil)

	resp = w.Result()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// no token
	r = httptest.NewRequest("POST", "scheme://host/path", nil)
	w = httptest.NewRecorder()

	wallet.ExtractToken(s.SignTx)(w, r, nil)

	resp = w.Result()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// token but invalid payload
	payload := `{"t": "some data", "pubKey": "asdasasdasd"}`
	r = httptest.NewRequest("POST", "scheme://host/path", bytes.NewBufferString(payload))
	w = httptest.NewRecorder()
	r.Header.Set("Authorization", "Bearer eyXXzA")

	wallet.ExtractToken(s.SignTx)(w, r, nil)

	resp = w.Result()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	payload = `{"tx": "some data", "puey": "asdasasdasd"}`
	r = httptest.NewRequest("POST", "scheme://host/path", bytes.NewBufferString(payload))
	w = httptest.NewRecorder()
	r.Header.Set("Authorization", "Bearer eyXXzA")

	wallet.ExtractToken(s.SignTx)(w, r, nil)

	resp = w.Result()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

}
