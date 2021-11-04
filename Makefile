build:
	go build -o bin/bot ./cmd/bot

run: build
	./bin/bot --cfg=./cfg.json