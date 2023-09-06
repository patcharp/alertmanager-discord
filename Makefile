DOCKER_IMG=patcharp/alertmanager-discord
VERSION=$(shell git describe --always --dirty)

# CI automate version set
ifeq ($(CI), true)
	VERSION=$(CI_BUILD_REF_NAME)-build$(CI_PIPELINE_ID)
endif

.PHONY: clean hello docker

hello:
	echo "Hello Golang!!"

version:
	echo $(VERSION)

docker:
	docker build -t $(DOCKER_IMG):$(VERSION) .
