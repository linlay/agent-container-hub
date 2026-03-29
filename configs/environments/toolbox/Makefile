IMAGE_NAME ?= toolbox
TAG ?= latest
PYTHON_BASE_IMAGE ?= python:3.12-slim-bookworm
APT_MIRROR ?= https://mirrors.tuna.tsinghua.edu.cn/debian
APT_SECURITY_MIRROR ?= https://mirrors.tuna.tsinghua.edu.cn/debian-security
DBX_DOWNLOAD_URL ?=
HTTPX_DOWNLOAD_URL ?=
MOCK_VERSION ?= 0.1.0
MOCK_DOWNLOAD_URL ?=

.PHONY: build build-cn

# Usage:
#   make build-cn  # builds the CN-friendly variant of toolbox:latest

build:
	docker build \
		--tag $(IMAGE_NAME):$(TAG) \
		--build-arg PYTHON_BASE_IMAGE=$(PYTHON_BASE_IMAGE) \
		--build-arg DBX_DOWNLOAD_URL=$(DBX_DOWNLOAD_URL) \
		--build-arg HTTPX_DOWNLOAD_URL=$(HTTPX_DOWNLOAD_URL) \
		--build-arg MOCK_VERSION=$(MOCK_VERSION) \
		--build-arg MOCK_DOWNLOAD_URL=$(MOCK_DOWNLOAD_URL) \
		-f Dockerfile \
		.

build-cn:
	docker build \
		--tag $(IMAGE_NAME):$(TAG) \
		--build-arg PYTHON_BASE_IMAGE=$(PYTHON_BASE_IMAGE) \
		--build-arg APT_MIRROR=$(APT_MIRROR) \
		--build-arg APT_SECURITY_MIRROR=$(APT_SECURITY_MIRROR) \
		--build-arg DBX_DOWNLOAD_URL=$(DBX_DOWNLOAD_URL) \
		--build-arg HTTPX_DOWNLOAD_URL=$(HTTPX_DOWNLOAD_URL) \
		--build-arg MOCK_VERSION=$(MOCK_VERSION) \
		--build-arg MOCK_DOWNLOAD_URL=$(MOCK_DOWNLOAD_URL) \
		-f Dockerfile \
		.
