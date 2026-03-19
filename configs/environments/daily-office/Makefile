IMAGE_NAME ?= daily-office
TAG ?= latest
CN_TAG ?= cn
HIMALAYA_DOWNLOAD_URL ?=

.PHONY: build build-cn

build:
	docker build \
		--tag $(IMAGE_NAME):$(TAG) \
		-f Dockerfile \
		.

build-cn:
	docker build \
		--tag $(IMAGE_NAME):$(CN_TAG) \
		--build-arg APT_MIRROR=https://mirrors.tuna.tsinghua.edu.cn/debian \
		--build-arg APT_SECURITY_MIRROR=https://mirrors.tuna.tsinghua.edu.cn/debian-security \
		--build-arg PIP_INDEX_URL=https://pypi.tuna.tsinghua.edu.cn/simple \
		--build-arg NPM_REGISTRY=https://registry.npmmirror.com \
		$(if $(strip $(HIMALAYA_DOWNLOAD_URL)),--build-arg HIMALAYA_DOWNLOAD_URL=$(HIMALAYA_DOWNLOAD_URL),) \
		-f Dockerfile \
		.
