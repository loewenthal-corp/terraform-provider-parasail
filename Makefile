.PHONY: build test fmt tidy do lint security clean

TASK ?= ./bin/task

do:
	$(TASK) do

build:
	$(TASK) build

test:
	$(TASK) test

fmt:
	$(TASK) format

lint:
	$(TASK) lint

security:
	$(TASK) security

tidy:
	go mod tidy

clean:
	$(TASK) clean
