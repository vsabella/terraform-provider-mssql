#!/bin/bash

source .env
docker compose up -d --wait
CONTAINER_ID=$(docker compose ps -q)
docker exec ${CONTAINER_ID} \
  /opt/mssql-tools/bin/sqlcmd -C -S db -U sa -P ${MSSQL_SA_PASSWORD} -d master $@ -i setup.sql
