.PHONY: build up down clean

build:
	go build ./...

up:
	docker-compose up -d --build

down:
	docker-compose down

clean:
	docker-compose down -v
