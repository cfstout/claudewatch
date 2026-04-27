BIN_DIR    := $(HOME)/.local/bin
LOG_DIR    := $(HOME)/Library/Logs/claudewatch
PLIST_NAME := com.cfstout.claudewatch.plist
PLIST      := $(HOME)/Library/LaunchAgents/$(PLIST_NAME)

.PHONY: build test lint install uninstall reload status clean

build:
	go build -o claudewatch ./cmd/claudewatch

test:
	go test -race ./...

lint:
	go vet ./...

install: build
	install -d $(BIN_DIR)
	install -d $(LOG_DIR)
	install -m 0755 claudewatch $(BIN_DIR)/claudewatch
	ln -sfn $(BIN_DIR)/claudewatch $(BIN_DIR)/cw
	sed -e 's|@@BIN@@|$(BIN_DIR)/claudewatch|g' \
	    -e 's|@@LOG_DIR@@|$(LOG_DIR)|g' \
	    dist/$(PLIST_NAME).template > $(PLIST)
	launchctl unload $(PLIST) 2>/dev/null || true
	launchctl load $(PLIST)
	@echo ""
	@echo "Verify daemon: curl -fsS localhost:7777/healthz"

uninstall:
	launchctl unload $(PLIST) 2>/dev/null || true
	rm -f $(PLIST)
	rm -f $(BIN_DIR)/claudewatch $(BIN_DIR)/cw

reload: install

status:
	@launchctl list | grep claudewatch || echo "(not loaded)"
	@echo ""
	@curl -fsS localhost:7777/healthz || echo "(daemon not responding)"

clean:
	rm -f claudewatch
