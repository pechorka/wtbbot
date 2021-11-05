package moex

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/go-redis/cache/v8"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

var (
	ErrNotFound        = errors.New("not found")
	errCacheNotUpdated = errors.New("cache not updated")
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
)

const (
	BoardStock          = "TQBR"
	BoardForeignStock   = "FQBR"
	BoardIndex          = "TQTF"
	BoardTreasuries     = "TQOB"
	BoardCorporateBonds = "TQCB"
)

type API struct {
	client *http.Client
	cache  *cache.Cache
}

type Opts struct {
	Client *http.Client
	Cache  *cache.Cache
}

func New(opts Opts) *API {
	api := API{
		client: opts.Client,
		cache:  opts.Cache,
	}

	if api.client == nil {
		api.client = http.DefaultClient
	}

	return &api
}

type StockInfo struct {
	Price     float64
	ShortName string
	LotSize   float64
}

func (api *API) Get(ctx context.Context, secid string) (*StockInfo, error) {
	s, err := api.getFromCache(ctx, secid)
	if err == nil {
		return s, nil
	}

	return api.getFromMoex(ctx, secid)
}

func (api *API) GetMultiple(ctx context.Context, secids ...string) (map[string]StockInfo, error) {
	res := make(map[string]StockInfo)
	for _, secid := range secids {
		s, err := api.Get(ctx, secid)
		if err != nil {
			if err == ErrNotFound {
				continue
			}
			return nil, err
		}
		res[secid] = *s
	}
	return res, nil
}

func (api *API) getFromMoex(ctx context.Context, secid string) (*StockInfo, error) {
	if err := api.UpdateCache(ctx); err != nil {
		return nil, err
	}

	return api.getFromCache(ctx, secid)
}

func (api *API) UpdateCache(ctx context.Context) error {
	gr, ectx := errgroup.WithContext(ctx)

	loadAndCache := func(ctx context.Context, engine, market, board string) error {
		data, err := api.loadSecuritiesPrices(ctx, engine, market, board)
		if err != nil {
			log.Printf("[ERROR] while loading for engine: %s, market: %s, board: %s, err: %v\n", engine, market, board, err)
			return err
		}

		return api.cacheData(ctx, data)
	}
	gr.Go(func() error {
		return loadAndCache(ectx, EngineStock, MarketShares, BoardStock)
	})
	gr.Go(func() error {
		return loadAndCache(ectx, EngineStock, MarketBonds, BoardTreasuries)
	})
	gr.Go(func() error {
		return loadAndCache(ectx, EngineStock, MarketBonds, BoardCorporateBonds)
	})
	gr.Go(func() error {
		return loadAndCache(ectx, EngineStock, MarketShares, BoardIndex)
	})
	gr.Go(func() error {
		return loadAndCache(ectx, EngineStock, MarketForeignShares, BoardForeignStock)
	})

	return gr.Wait()
}

func (api *API) loadSecuritiesPrices(ctx context.Context, engine, market, board string) (map[string]StockInfo, error) {
	urlStr := "http://iss.moex.com/iss/engines/" + engine + "/markets/" + market + "/boards/" + board + "/securities.json?iss.meta=off&iss.only=securities"

	var respBody struct {
		Securities struct {
			Columns []string        `json:"columns"`
			Data    [][]interface{} `json:"data"`
		} `json:"securities"`
	}

	if err := api.get(ctx, urlStr, &respBody); err != nil {
		return nil, errors.Wrap(err, "error while parsing response body")
	}

	var (
		secidIndex     int
		shortNameIndex int
		lotSizeIndex   int
		priceIndex     int
	)

	for i, column := range respBody.Securities.Columns {
		switch column {
		case "SECID":
			secidIndex = i
		case "SHORTNAME":
			shortNameIndex = i
		case "LOTSIZE":
			lotSizeIndex = i
		case "PREVADMITTEDQUOTE":
			priceIndex = i
		}
	}

	res := make(map[string]StockInfo, len(respBody.Securities.Data))
	for i, data := range respBody.Securities.Data {
		if data[priceIndex] == nil { //price not available
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

		prevPrice, ok := data[priceIndex].(float64)
		if !ok {
			return nil, errors.Errorf("PREVWAPRICE for data %d is not a number, got %T", i, data[priceIndex])
		}

		lotSize, ok := data[lotSizeIndex].(float64)
		if !ok {
			return nil, errors.Errorf("LOTSIZE for data %d is not a number, got %T", i, data[lotSizeIndex])
		}

		res[secid] = StockInfo{
			Price:     prevPrice,
			ShortName: shortName,
			LotSize:   lotSize,
		}
	}

	return res, nil
}

func (api *API) cacheData(ctx context.Context, data map[string]StockInfo) error {
	for secid, info := range data {
		item := cache.Item{
			Ctx:   ctx,
			Key:   secid,
			Value: info,
			TTL:   24 * time.Hour,
		}
		if err := api.cache.Set(&item); err != nil {
			return err
		}
	}

	return nil
}

func (api *API) getFromCache(ctx context.Context, secID string) (*StockInfo, error) {
	var s StockInfo
	err := api.cache.Get(ctx, secID, &s)
	if err != nil {
		if errors.Is(err, cache.ErrCacheMiss) {
			return nil, ErrNotFound
		}
		return nil, errors.Wrap(err, "error while retriving from cache")
	}

	return &s, nil
}

func (api *API) get(ctx context.Context, urlStr string, respBody interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return errors.Wrap(err, "error creating req")
	}

	resp, err := api.client.Do(req)
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
