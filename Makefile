build:
	go build -o bin/bot ./cmd/bot

run:
	./bin/bot --cfg=./cfg.json

brun: build run