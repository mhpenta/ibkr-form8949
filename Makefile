.PHONY: all build main activity test clean

all: build

build: main activity

main:
	go build -o ibkr-form8949 .

activity:
	go build -o ibkr-activity ./cmd/activity

test:
	go test ./...

clean:
	rm -f ibkr-form8949 ibkrForm8949 ibkr-activity ibkranalyzer
