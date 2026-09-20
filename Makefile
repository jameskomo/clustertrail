# Go and kind are often installed under the home directory rather than on the
# system PATH; find them without asking the user to export anything.
export PATH := $(HOME)/.local/bin:$(HOME)/.local/go/bin:$(PATH)

GO      ?= go
NPM     ?= npm
VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: dev engine app bundle run demo record test integration churn clean docs help

help:   ; @echo "make bundle   one binary with the interface inside it (what users download)" \
	  && echo "make run      build that binary and open it in your browser" \
	  && echo "make dev      development: engine plus a hot-reloading UI" \
	  && echo "make test     go vet and the Go test suite" \
	  && echo "make integration  the tests that need a live cluster" \
	  && echo "make demo     the public interactive demo, into site/demo" \
	  && echo "make check-demo  drive every screen of the demo and assert it answers" \
	  && echo "make record   re-record the demo fixtures from a running engine" \
	  && echo "make shots    re-take the screenshots the site and README publish" \
	  && echo "make stamp    content-stamp the page assets so a change reaches the edge" \
	  && echo "make docs     list the documentation"

## Development: two processes, the UI hot-reloads.
dev:     ; ./scripts/dev.sh

## One self-contained binary: build the UI, embed it, compile.
bundle:
	cd app && $(NPM) ci --silent || $(NPM) install --silent
	cd app && $(NPM) run build
	rm -rf engine/internal/ui/dist
	cp -r app/dist engine/internal/ui/dist
	cd engine && $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/clustertrail ./cmd/clustertrail
	@echo "built engine/bin/clustertrail ($$(du -h engine/bin/clustertrail | cut -f1))"

## The interactive demo: the real interface, recorded fixtures, no backend.
demo:
	cd app && VITE_DEMO=1 $(NPM) run build -- --base=/demo/ --outDir ../site/demo --emptyOutDir
	@echo "built site/demo ($$(du -sh site/demo | cut -f1))"

## Drive every screen of the demo and assert it answers.
check-demo:
	cd app && VITE_DEMO=1 $(NPM) run build -- --base=/demo/ --outDir ../site/demo --emptyOutDir
	node scripts/check-demo.mjs

## Re-take the screenshots the site and the README publish.
shots:
	node scripts/shoot.mjs
	node scripts/stamp-site.mjs

## Stamp a content hash onto the page's assets, so a change reaches people
## the edge has already cached.
stamp:
	node scripts/stamp-site.mjs

## Re-record the demo fixtures from a running engine.
record:
	node scripts/record-demo.mjs > app/src/demo/fixtures.json
	@echo "recorded app/src/demo/fixtures.json"

## Build it and open it, the way a user runs it.
run: bundle
	./engine/bin/clustertrail serve --open

engine:  ; cd engine && $(GO) build -o bin/clustertrail ./cmd/clustertrail
app:     ; cd app && $(NPM) run build
test:    ; cd engine && $(GO) vet ./... && $(GO) test ./...

## The tests that need a live API server. `make dev` leaves a cluster behind.
integration:
	cd engine && KUBECONFIG=$(PWD)/.kind-kubeconfig CLUSTERTRAIL_CONTEXT=kind-clustertrail \
		$(GO) test -tags integration ./internal/integration/ -v
churn:   ; ./scripts/churn.sh
docs:    ; @echo "Documentation:" && ls docs/*.md | sed "s|^|  |"
clean:   ; rm -rf engine/bin app/dist && git checkout -- engine/internal/ui/dist 2>/dev/null || true
