.PHONY: build pack
build:
	mkdir -p bin
	go build -trimpath -o bin/rclone-worker ./cmd/rclone-worker

pack:
	bash scripts/pack.sh dist
