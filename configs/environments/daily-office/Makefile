IMAGE_NAME ?= daily-office
TAG ?= latest
CN_TAG ?= cn
HIMALAYA_DOWNLOAD_URL ?=
CN_HIMALAYA_DOWNLOAD_URL ?= $(HIMALAYA_DOWNLOAD_URL)
CN_PYTHON_BASE_IMAGE ?= docker.m.daocloud.io/python:3.12-slim-bookworm

.PHONY: build build-cn check-cn-himalaya-url

# Usage:
#   make build-cn CN_HIMALAYA_DOWNLOAD_URL=https://<mirror>/himalaya-archive.tar.gz
# Override CN_PYTHON_BASE_IMAGE if you need a different mainland-accessible mirror.

check-cn-himalaya-url:
	@if [ -z "$(strip $(CN_HIMALAYA_DOWNLOAD_URL))" ]; then \
		echo "build-cn requires CN_HIMALAYA_DOWNLOAD_URL (or HIMALAYA_DOWNLOAD_URL) because the Dockerfile otherwise downloads Himalaya from GitHub Release."; \
		echo "Example: make build-cn CN_HIMALAYA_DOWNLOAD_URL=https://<mirror>/himalaya-v1.2.0-x86_64-unknown-linux-gnu.tar.gz"; \
		exit 1; \
	fi

build:
	docker build \
		--tag $(IMAGE_NAME):$(TAG) \
		-f Dockerfile \
		.

build-cn: check-cn-himalaya-url
	docker build \
		--tag $(IMAGE_NAME):$(CN_TAG) \
		--build-arg PYTHON_BASE_IMAGE=$(CN_PYTHON_BASE_IMAGE) \
		--build-arg APT_MIRROR=https://mirrors.tuna.tsinghua.edu.cn/debian \
		--build-arg APT_SECURITY_MIRROR=https://mirrors.tuna.tsinghua.edu.cn/debian-security \
		--build-arg PIP_INDEX_URL=https://pypi.tuna.tsinghua.edu.cn/simple \
		--build-arg NPM_REGISTRY=https://registry.npmmirror.com \
		--build-arg HIMALAYA_DOWNLOAD_URL=$(CN_HIMALAYA_DOWNLOAD_URL) \
		-f Dockerfile \
		.
