.PHONY: build test clean install uninstall

BINARY_NAME=gmove
INSTALL_DIR?=$(HOME)/.local/bin

build:
	go build -o $(BINARY_NAME) ./cmd/gmove

test:
	go test -v ./tests

clean:
	rm -f $(BINARY_NAME) *.db *.log

install: build
	mkdir -p $(INSTALL_DIR)
	install -m 755 $(BINARY_NAME) $(INSTALL_DIR)/$(BINARY_NAME)
	@echo "Installed $(BINARY_NAME) to $(INSTALL_DIR)/$(BINARY_NAME)"

uninstall:
	rm -f $(INSTALL_DIR)/$(BINARY_NAME)
	@echo "Uninstalled $(BINARY_NAME) from $(INSTALL_DIR)"
