SHELL := /bin/sh

.PHONY: server-build server-run cli-check web-build web-serve db-migrate db-seed db-setup build check

server-build:
	go -C server build ./...

server-run:
	go -C server run ./cmd/kura-server

cli-check:
	go -C cli test ./...
	go -C cli build ./...

web-build:
	mkdir -p web/dist
	cd web && elm make src/Main.elm --output=dist/elm.js

web-serve:
	python3 -m http.server 8000 --directory web

db-migrate:
	./scripts/db-migrate.sh

db-seed:
	./scripts/db-seed.sh

db-setup: db-migrate db-seed

build: server-build web-build

check: server-build cli-check web-build
