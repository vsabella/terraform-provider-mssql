#!/bin/bash

(
    cd docker_compose
    ./start.sh
)
export $(cat docker_compose/.env | xargs) && TF_ACC=1 go test ./... -v $(TESTARGS) -timeout 120m;
(
    cd docker_compose
  	./stop.sh;
)
