package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pechorka/whattobuy/moex"
	"github.com/pechorka/whattobuy/store"
	"github.com/pkg/errors"
)

type config struct {
	Token            string `json:"token"`
	TimeoutSec       int    `json:"timeout_seconds"`
	CacheMOEXAPIResp bool   `json:"cache_moex_api_resp"`
	StorePath        string `json:"store_path"`
}

func main() {
	if err := run(); err != nil {
		log.Println(err)
		os.Exit(1)
	}
	os.Exit(0)
}

func run() error {
	cfgPath := flag.String("cfg", "", "")
	flag.Parse()
	f, err := os.Open(*cfgPath)
	if err != nil {
		return errors.Wrap(err, "error while opening bot")
	}

	var cfg config
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return errors.Wrap(err, "error while decoding cfg")
	}

	if err := f.Close(); err != nil {
		return errors.Wrap(err, "error while closing cfg file")
	}

	store, err := store.New(cfg.StorePath)
	if err != nil {
		return errors.Wrap(err, "error while initializing store")
	}
	defer func() {
		cerr := store.Close()
		if cerr != nil {
			log.Println("[ERROR] error while closing store: ", cerr.Error())
		}
		log.Println("[INFO] closed storage")
	}()
	log.Println("[INFO] opened storage")

	api := moex.New(moex.Opts{ToCache: cfg.CacheMOEXAPIResp})

	b, err := NewBot(Opts{
		Token:   cfg.Token,
		Timeout: time.Duration(cfg.TimeoutSec) * time.Second,
		Store:   store,
		MoexAPI: api,
	})

	if err != nil {
		return errors.Wrap(err, "error while initializing bot")
	}

	go b.Start()
	defer func() {
		b.Stop()
		log.Println("[INFO] stoped bot")
	}()

	log.Println("[INFO] started bot")

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	<-shutdown

	return nil
}
