APP_NAME := geoip-auth-server
DOCKER_IMAGE := yourdockerhubusername/geoip-auth:latest

.PHONY: build test docker-build docker-push run

build:
	go build -o $(APP_NAME)

test:
	go test -count=1 ./...

cover:
	go test -count=1 -cover ./...

run:
	./$(APP_NAME)

docker-build:
	docker build -t $(DOCKER_IMAGE) .

docker-push:
	docker push $(DOCKER_IMAGE)
