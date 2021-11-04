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

	api := New(Opts{Client: server.Client()})

	prices, err := api.GetAllSecuritiesPrices(EngineStock, MarketShares)
	if err != nil {
		t.Fatal(err)
	}

	expected := map[string]StockInfo{
		"AFKS": {
			Price:     27.422,
			ShortName: "Система ао",
		},
	}

	for expectedSecid, expected := range expected {
		info, ok := prices[expectedSecid]
		if !ok {
			t.Errorf("expected to get secid %s", expectedSecid)
			continue
		}

		if info.Price != expected.Price {
			t.Errorf("expected price %f, got %f", expected.Price, info.Price)
		}
		if info.ShortName != expected.ShortName {
			t.Errorf("expected name %q, got %q", expected.ShortName, info.ShortName)
		}
	}
}
