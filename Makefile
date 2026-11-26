BIN := $(HOME)/.local/bin/infiterm
PORT ?= 7681

.PHONY: build run test install uninstall

build:
	go build -o bin/infiterm .

run: build
	./bin/infiterm -port $(PORT)

test:
	go test -v -race ./...

# 编译到 ~/.local/bin 并注册 launchd：登录后自动启动；改代码后再执行一次即可更新并重启
install:
	mkdir -p $(dir $(BIN))
	go build -o $(BIN) .
	$(BIN) install -port $(PORT)

uninstall:
	$(BIN) uninstall
	rm -f $(BIN)
