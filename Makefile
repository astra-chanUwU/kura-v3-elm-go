SHELL := /bin/sh

.PHONY: server-build server-run server-check cli-check web-build web-test web-serve db-migrate db-seed db-setup build check

server-build:
	go -C server build ./...

server-run:
	go -C server run ./cmd/kura-server

server-check:
	go -C server test -p 1 ./...
	go -C server vet ./...

cli-check:
	go -C cli test ./...
	go -C cli build ./...

web-build:
	mkdir -p web/dist
	cd web && elm make src/Main.elm --output=dist/elm.js

web-test:
	mkdir -p web/dist
	cd web && elm make tests/TestRunner.elm --output=dist/elm-tests.js
	cd web && node tests/run.mjs

web-serve:
	go -C server run ./cmd/kura-web

db-migrate:
	./scripts/db-migrate.sh

db-seed:
	./scripts/db-seed.sh

db-setup: db-migrate db-seed

build: server-build web-build

check: server-build server-check cli-check web-build web-test
