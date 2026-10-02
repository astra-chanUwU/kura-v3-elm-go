SHELL := /bin/sh

.PHONY: server-build server-run web-build web-serve build check

server-build:
	go -C server build ./...

server-run:
	go -C server run ./cmd/kura-server

web-build:
	mkdir -p web/dist
	cd web && elm make src/Main.elm --output=dist/elm.js

web-serve:
	python3 -m http.server 8000 --directory web

build: server-build web-build

check: server-build web-build
