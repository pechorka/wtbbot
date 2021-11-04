package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/go-redis/cache/v8"
	"github.com/go-redis/redis/v8"
	"github.com/pechorka/whattobuy/moex"
	"github.com/pechorka/whattobuy/store"
	"github.com/pkg/errors"
)

type config struct {
	Token      string `json:"token"`
	TimeoutSec int    `json:"timeout_seconds"`
	StorePath  string `json:"store_path"`
	RedisAddr  string `json:"redis_addr"`
	WebHookURL string `json:"web_hook_url"`
	TLSKey     string `json:"tls_key"`
	TLSCert    string `json:"tls_cert"`
}

func main() {
	if err := run(); err != nil {
		log.Println(err)
		os.Exit(1)
	}
	os.Exit(0)
}

func run() error {
	ctx := context.Background()
	cfgPath := flag.String("cfg", "", "")
	flag.Parse()
	cfg, err := readConfig(*cfgPath)
	if err != nil {
		return err
	}
	log.Printf("%+v", cfg)

	store, err := store.New(cfg.StorePath)
	if err != nil {
		return errors.Wrap(err, "error while initializing store")
	}
	defer func() {
		cerr := store.Close()
		if cerr != nil {
			log.Println("[ERROR] error while closing store: ", cerr.Error())
			return
		}
		log.Println("[INFO] closed storage")
	}()
	log.Println("[INFO] opened storage")

	redisCLI := redis.NewClient(&redis.Options{
		Addr: cfg.RedisAddr,
	})

	if err := redisCLI.Ping(ctx).Err(); err != nil {
		return errors.Wrap(err, "error while pinging redis server")
	}
	log.Println("[INFO] connected to redis on ", cfg.RedisAddr)
	defer func() {
		cerr := redisCLI.Close()
		if cerr != nil {
			log.Println("[ERROR] error while closing redis connection: ", cerr.Error())
			return
		}
		log.Println("[INFO] closed redis connection")
	}()

	redisCache := cache.New(&cache.Options{
		Redis: redisCLI,
	})

	api := moex.New(moex.Opts{Cache: redisCache})

	b, err := NewBot(&Opts{
		Token:      cfg.Token,
		Timeout:    time.Duration(cfg.TimeoutSec) * time.Second,
		Store:      store,
		MoexAPI:    api,
		WebHookURL: cfg.WebHookURL,
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

func readConfig(path string) (*config, error) {
	// TODO use lib for configs
	var cfg config
	switch path {
	case "":
		timeout, _ := strconv.Atoi(os.Getenv("TIMEOUT_SECONDS"))
		cfg = config{
			Token:      os.Getenv("TOKEN"),
			TimeoutSec: timeout,
			StorePath:  os.Getenv("STORE_PATH"),
			RedisAddr:  os.Getenv("REDIS_ADDR"),
			WebHookURL: os.Getenv("WEBHOOKURL"),
			TLSKey:     os.Getenv("TLSKEY"),
			TLSCert:    os.Getenv("TLSCERT"),
		}
	default:
		f, err := os.Open(path)
		if err != nil {
			return nil, errors.Wrap(err, "error while opening bot config file")
		}

		if err := json.NewDecoder(f).Decode(&cfg); err != nil {
			return nil, errors.Wrap(err, "error while decoding cfg")
		}

		if err := f.Close(); err != nil {
			return nil, errors.Wrap(err, "error while closing cfg file")
		}
	}

	return &cfg, nil
}
