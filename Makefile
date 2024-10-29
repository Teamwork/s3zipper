NAME        = s3zipper
VERSION     = $(shell git rev-parse HEAD)
BRANCH      = $(shell git rev-parse --abbrev-ref HEAD)

IMAGE_REPO  = teamwork/$(NAME)
CACHE_TAG   = $(IMAGE_REPO):cache-$(BRANCH)
TAG         = $(IMAGE_REPO):$(VERSION)

ECR_IMAGE_REPO  = $(ECR_ACCOUNT).dkr.ecr.$(ECR_REGION).amazonaws.com/teamwork/$(NAME)
ECR_CACHE_TAG   = $(ECR_IMAGE_REPO):cache-$(BRANCH)
ECR_TAG         = $(ECR_IMAGE_REPO):$(VERSION)

.PHONY: default install build push chart-update git-prep git-push install-docker install-buildx install-yq

#
# Global targets
#
default: build

install: install-docker install-buildx install-yq

#
# Docker image building and pushing
#
# * https://github.com/docker/buildx
#
build:
	docker buildx build \
	  --build-arg BUILD_DATE=$(shell date --iso-8601=minutes) \
	  --build-arg BUILD_VCS_REF=$(shell git rev-parse --short HEAD) \
	  --build-arg BUILD_VERSION=$(VERSION) \
	  -t $(TAG) \
	  -t $(ECR_TAG) \
	  --load \
	  .

push:
	docker buildx build \
	  --build-arg BUILD_DATE=$(shell date --iso-8601=minutes) \
	  --build-arg BUILD_VCS_REF=$(shell git rev-parse --short HEAD) \
	  --build-arg BUILD_VERSION=$(VERSION) \
	  --cache-from=type=registry,ref=$(CACHE_TAG) \
	  --cache-to=type=registry,ref=$(CACHE_TAG),mode=max \
	  -t $(TAG) \
	  -t $(ECR_TAG) \
	  --push \
	  --progress=plain \
	  .

#
# Helm chart updates
#
chart-update:
	yq eval -i '.appVersion = "$(VERSION)"' docker/helm/Chart.yaml
	yq eval -i '.appVersion = "$(VERSION)"' docker/helm-eks/Chart.yaml

#
# GitOps deployment will be triggered by a committed change to helm chart
#
git-prep:
	git config --global user.email "gitops@teamwork.com"
	git config --global user.name "GitOps CI"
	git remote add gh https://$(GH_TOKEN)@github.com/Teamwork/$(NAME).git > /dev/null 2>&1
	git pull gh $(BRANCH) --ff-only

git-push: chart-update
	git commit -am "[ci skip] Updated helm chart to $(VERSION)"
	git push gh HEAD:$(BRANCH)

#
# Install dependencies
#
install-yq:
	sudo add-apt-repository ppa:rmescandon/yq -y
	sudo apt update
	sudo apt install yq -y

install-docker:
	curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo apt-key add -
	sudo add-apt-repository "deb [arch=amd64] https://download.docker.com/linux/ubuntu $(shell lsb_release -cs) stable"
	sudo apt-get update
	sudo apt-get -y -o Dpkg::Options::="--force-confnew" install docker-ce

install-buildx:
	mkdir -p ~/.docker/cli-plugins
	curl -L https://github.com/docker/buildx/releases/download/v0.3.1/buildx-v0.3.1.linux-amd64 -o ~/.docker/cli-plugins/docker-buildx
	chmod 755 ~/.docker/cli-plugins/docker-buildx
	docker buildx create --name container --use
