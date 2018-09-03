#!/usr/bin/env bash
docker build "${@}" -t minienv/minienv-api:0.2.0-DEV ./
docker push minienv/minienv-api:0.2.0-DEV