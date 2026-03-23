IMAGE_NAME ?= daily-office
TAG ?= latest
PYTHON_BASE_IMAGE ?= python:3.12-slim-bookworm
APT_MIRROR ?= https://mirrors.tuna.tsinghua.edu.cn/debian
APT_SECURITY_MIRROR ?= https://mirrors.tuna.tsinghua.edu.cn/debian-security
PIP_INDEX_URL ?= https://pypi.tuna.tsinghua.edu.cn/simple
NPM_REGISTRY ?= https://registry.npmmirror.com
HIMALAYA_DOWNLOAD_URL ?= https://cligrep.com/cli-releases/himalaya.aarch64-linux.tgz

.PHONY: build build-cn

# Usage:
#   make build-cn  # builds the CN-friendly variant of daily-office:latest

build:
	docker build \
		--tag $(IMAGE_NAME):$(TAG) \
		--build-arg PYTHON_BASE_IMAGE=$(PYTHON_BASE_IMAGE) \
		-f Dockerfile \
		.

build-cn:
	docker build \
		--tag $(IMAGE_NAME):$(TAG) \
		--build-arg PYTHON_BASE_IMAGE=$(PYTHON_BASE_IMAGE) \
		--build-arg APT_MIRROR=$(APT_MIRROR) \
		--build-arg APT_SECURITY_MIRROR=$(APT_SECURITY_MIRROR) \
		--build-arg PIP_INDEX_URL=$(PIP_INDEX_URL) \
		--build-arg NPM_REGISTRY=$(NPM_REGISTRY) \
		--build-arg HIMALAYA_DOWNLOAD_URL=$(HIMALAYA_DOWNLOAD_URL) \
		-f Dockerfile \
		.
