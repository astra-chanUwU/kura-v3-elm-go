SHELL := /bin/sh

.PHONY: server-build server-run server-check cli-check web-build web-test web-import-check web-serve db-migrate db-seed db-setup build check script-check library-drill

server-build:
	go -C server build ./...

server-run:
	go -C server run ./cmd/kura-server

server-check:
	go -C server test -p 1 ./...
	go -C server vet ./...

cli-check:
	go -C cli test ./...
	go -C cli vet ./...
	go -C cli build ./...

web-build:
	mkdir -p web/dist
	cd web && elm make src/Main.elm --output=dist/elm.js

web-import-check:
	cd web && node scripts/check-import-boundaries.mjs

web-test: web-import-check
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

script-check:
	PYTHONPATH=scripts python3 -m unittest test_library_backup test_import_test_media test_import_media

# This creates unique disposable databases on the explicitly supplied local
# PostgreSQL instance; it never uses the development library database.
library-drill:
	python3 -c 'import os; assert os.environ.get("KURA_BACKUP_TEST_ADMIN_URL"), "Set an explicitly disposable local KURA_BACKUP_TEST_ADMIN_URL"'
	python3 -m unittest discover -s scripts -p '*integration.py' -v

check: server-build server-check cli-check web-build web-test script-check
