IMAGE_NAME ?= daily-office
TAG ?= latest
HIMALAYA_DOWNLOAD_URL ?= https://cligrep.com/cli-releases/himalaya.aarch64-linux.tgz

.PHONY: build build-cn

# Usage:
#   make build-cn  # builds the CN-friendly variant of daily-office:latest

build:
	docker build \
		--tag $(IMAGE_NAME):$(TAG) \
		-f Dockerfile \
		.

build-cn:
	docker build \
		--tag $(IMAGE_NAME):$(TAG) \
		--build-arg PYTHON_BASE_IMAGE=docker.1ms.run/library/python:3.12-slim-bookworm \
		--build-arg APT_MIRROR=https://mirrors.tuna.tsinghua.edu.cn/debian \
		--build-arg APT_SECURITY_MIRROR=https://mirrors.tuna.tsinghua.edu.cn/debian-security \
		--build-arg PIP_INDEX_URL=https://pypi.tuna.tsinghua.edu.cn/simple \
		--build-arg NPM_REGISTRY=https://registry.npmmirror.com \
		--build-arg HIMALAYA_DOWNLOAD_URL=$(HIMALAYA_DOWNLOAD_URL) \
		-f Dockerfile \
		.
