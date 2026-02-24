# MADFLOW Makefile
# Provides common development commands for building and managing the MADFLOW binary.

BINARY := madflow
CMD_DIR := ./cmd/madflow
GOFLAGS ?=
INSTALL_DIR ?= $(HOME)/bin

# Default target: build
.PHONY: all
all: build

# build compiles the madflow binary from source.
.PHONY: build
build:
	go build $(GOFLAGS) -o $(BINARY) $(CMD_DIR)
	@echo "Built $(BINARY) ($(shell stat -c '%y' $(BINARY) | cut -d' ' -f1,2 | cut -d'.' -f1))"

# install copies the built binary to INSTALL_DIR (default: ~/bin).
# Run after build to update the system-wide binary used by the running process.
.PHONY: install
install: build
	@mkdir -p $(INSTALL_DIR)
	install -m 755 $(BINARY) $(INSTALL_DIR)/$(BINARY)
	@echo "Installed $(BINARY) to $(INSTALL_DIR)/$(BINARY)"

# test runs all unit and integration tests.
.PHONY: test
test:
	go test ./...

# lint runs gofmt and go vet.
.PHONY: lint
lint:
	gofmt -l ./... | grep . && exit 1 || true
	go vet ./...

# clean removes the built binary.
.PHONY: clean
clean:
	rm -f $(BINARY)

# rebuild: rebuild, install to INSTALL_DIR, and stop the running madflow process.
# The supervisor process (e.g. systemd or a shell loop) should restart it automatically.
# Set INSTALL_DIR to override the default install location (~/bin).
.PHONY: rebuild
rebuild: install
	@PID=$$(pgrep -f 'madflow start' -n 2>/dev/null); \
	if [ -n "$$PID" ]; then \
		echo "Stopping madflow (PID $$PID)..."; \
		kill -TERM $$PID; \
		sleep 2; \
	else \
		echo "No running madflow process found."; \
	fi

# restart: restart the madflow process using the current binary.
# Sends SIGTERM to the running process so it can be restarted manually.
.PHONY: restart
restart:
	@PID=$$(pgrep -f './madflow start' -n 2>/dev/null || pgrep -f 'madflow start' -n 2>/dev/null); \
	if [ -n "$$PID" ]; then \
		echo "Stopping madflow (PID $$PID)..."; \
		kill -TERM $$PID; \
		echo "Process stopped. Please restart with: ./madflow start"; \
	else \
		echo "No running madflow process found."; \
	fi

.PHONY: help
help:
	@echo "MADFLOW Makefile targets:"
	@echo "  make build    - Build the madflow binary"
	@echo "  make install  - Build and install binary to INSTALL_DIR (default: ~/bin)"
	@echo "  make test     - Run all tests"
	@echo "  make lint     - Run gofmt and go vet"
	@echo "  make clean    - Remove the built binary"
	@echo "  make rebuild  - Build, install, and stop running process (for restart by supervisor)"
	@echo "  make restart  - Stop the running madflow process"
	@echo "  make help     - Show this help message"
