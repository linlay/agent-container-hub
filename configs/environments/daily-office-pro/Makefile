IMAGE_NAME ?= daily-office-pro
TAG ?= latest
PYTHON_BASE_IMAGE ?= python:3.12-slim-bookworm
APT_MIRROR ?= https://mirrors.tuna.tsinghua.edu.cn/debian
APT_SECURITY_MIRROR ?= https://mirrors.tuna.tsinghua.edu.cn/debian-security
PIP_INDEX_URL ?= https://pypi.tuna.tsinghua.edu.cn/simple
NPM_REGISTRY ?= https://registry.npmmirror.com
HIMALAYA_DOWNLOAD_URL ?=
DBX_DOWNLOAD_URL ?=
HTTPX_DOWNLOAD_URL ?=
DOTNET_SDK_PACKAGE ?=

.PHONY: build build-cn

# Usage:
#   make build-cn  # builds the CN-friendly variant of daily-office-pro:latest

build:
	docker build \
		--tag $(IMAGE_NAME):$(TAG) \
		--build-arg PYTHON_BASE_IMAGE=$(PYTHON_BASE_IMAGE) \
		--build-arg HIMALAYA_DOWNLOAD_URL=$(HIMALAYA_DOWNLOAD_URL) \
		--build-arg DBX_DOWNLOAD_URL=$(DBX_DOWNLOAD_URL) \
		--build-arg HTTPX_DOWNLOAD_URL=$(HTTPX_DOWNLOAD_URL) \
		--build-arg DOTNET_SDK_PACKAGE=$(DOTNET_SDK_PACKAGE) \
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
		--build-arg DBX_DOWNLOAD_URL=$(DBX_DOWNLOAD_URL) \
		--build-arg HTTPX_DOWNLOAD_URL=$(HTTPX_DOWNLOAD_URL) \
		--build-arg DOTNET_SDK_PACKAGE=$(DOTNET_SDK_PACKAGE) \
		-f Dockerfile \
		.
