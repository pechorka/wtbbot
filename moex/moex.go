package moex

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"

	"github.com/pkg/errors"
)

// all engines are listed here http://iss.moex.com/iss/engines
const (
	EngineStock    = "stock"
	EngineCurrency = "currency"
)

// all markets are listed here http://iss.moex.com/iss/engines/stock/markets
const (
	MarketShares        = "shares"
	MarketBonds         = "bonds"
	MarketIndex         = "index"
	MarketForeignShares = "foreignshares"
	MarketForeignndm    = "foreignndm"
)

type API struct {
	Client *http.Client
}

type StockInfo struct {
	Price     float64
	ShortName string
}

func (api *API) GetAllSecuritiesPrices(engine, market string) (map[string]StockInfo, error) {
	urlStr := "http://iss.moex.com/iss/engines/" + engine + "/markets/" + market + "/securities.json"

	var respBody struct {
		Securities struct {
			Data [][]interface{} `json:"data"`
		} `json:"securities"`
	}

	if err := api.get(urlStr, &respBody); err != nil {
		return nil, errors.Wrap(err, "error while parsing response body")
	}

	const (
		secidIndex     = 0
		shortNameIndex = 2
		prevPriceIndex = 15
	)

	res := make(map[string]StockInfo)

	for i, data := range respBody.Securities.Data {
		if data[prevPriceIndex] == nil { //price not available
			continue
		}

		secid, ok := data[secidIndex].(string)
		if !ok {
			return nil, errors.Errorf("SECID for data %d is not a string, got %T", i, data[secidIndex])
		}

		shortName, ok := data[shortNameIndex].(string)
		if !ok {
			return nil, errors.Errorf("SHORTNAME for data %d is not a string, got %T", i, data[shortNameIndex])
		}

		prevPrice, ok := data[prevPriceIndex].(float64)
		if !ok {
			return nil, errors.Errorf("PREVWAPRICE for data %d is not a number, got %T", i, data[prevPriceIndex])
		}

		res[secid] = StockInfo{
			Price:     prevPrice,
			ShortName: shortName,
		}
	}

	return res, nil
}

func (api *API) get(urlStr string, respBody interface{}) error {
	u, err := url.Parse(urlStr)
	if err != nil {
		return errors.Wrap(err, "error while parsing url")
	}

	resp, err := api.Client.Get(u.String())
	if err != nil {
		return errors.Wrap(err, "fetching data from moex")
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Println("[ERROR] error while closing response body:", err)
		}
	}()

	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return errors.Wrap(err, "error while parsing response body")
	}

	return nil
}
