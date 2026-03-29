IMAGE_NAME ?= shell
TAG ?= latest
BASE_IMAGE ?= busybox:latest
CN_BASE_IMAGE ?= m.daocloud.io/docker.io/library/busybox:latest

.PHONY: build build-cn

# Usage:
#   make build-cn  # override CN_BASE_IMAGE=... if your preferred mirror differs

build:
	docker build \
		--tag $(IMAGE_NAME):$(TAG) \
		--build-arg BASE_IMAGE=$(BASE_IMAGE) \
		-f Dockerfile \
		.

build-cn:
	docker build \
		--tag $(IMAGE_NAME):$(TAG) \
		--build-arg BASE_IMAGE=$(CN_BASE_IMAGE) \
		-f Dockerfile \
		.
