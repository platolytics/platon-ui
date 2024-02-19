.PHONE: all
all: run

build: generate
	go build

run: build
	./platon-ui

generate:
	go generate

hotreload:
	templ generate --watch --proxy="http://localhost:8080" --cmd="make run"
