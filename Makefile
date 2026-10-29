BIN := $(HOME)/.local/bin/porthole
PORT ?= 7681

.PHONY: build run install uninstall

build:
	go build -o bin/porthole .

run: build
	./bin/porthole -port $(PORT)

# 编译到 ~/.local/bin 并注册 launchd：登录后自动启动；改代码后再执行一次即可更新并重启
install:
	mkdir -p $(dir $(BIN))
	go build -o $(BIN) .
	$(BIN) install -port $(PORT)

uninstall:
	$(BIN) uninstall
	rm -f $(BIN)
