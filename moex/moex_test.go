package moex

import (
	_ "embed"
	"net/http"
	"net/http/httptest"
	"testing"
)

//go:embed test-resp.json
var getAllSecuritiesPricesResp string

func TestMoexAPI_GetAllSecuritiesPrices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(getAllSecuritiesPricesResp))
	}))

	t.Cleanup(server.Close)

	api := &MoexAPI{server.Client()}

	prices, err := api.GetAllSecuritiesPrices(EngineStock, MarketShares)
	if err != nil {
		t.Fatal(err)
	}

	expected := map[string]float64{
		"AFKS": 27.422,
	}

	for expectedSecid, expectedPrice := range expected {
		price, ok := prices[expectedSecid]
		if !ok {
			t.Errorf("expected to get secid %s", expectedSecid)
			continue
		}

		if price != expectedPrice {
			t.Errorf("expected price %f, got %f", expectedPrice, price)
		}
	}
}
