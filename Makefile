.PHONY: build pack docs-config
build:
	mkdir -p bin
	go build -trimpath -o bin/rclone-worker ./cmd/rclone-worker

pack:
	bash scripts/pack.sh dist

docs-config:
	python3 scripts/gen-readme-config.py .
