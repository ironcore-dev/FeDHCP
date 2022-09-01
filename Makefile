.PHONY: target/fedhcp

all: target/fedhcp

target/fedhcp:
	mkdir -p target
	CGO_ENABLED=0 go build -o target/fedhcp .

clean:
	rm -rf target

run: all
	sudo ./target/fedhcp

docker:
	docker build -t onmetal/fedhcp .