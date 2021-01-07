#!/bin/bash

DIR=$( cd $(dirname $0) ; pwd -P )
ROOT_DIR=$DIR/../../

echo "Building goaws..."
git clone git@github.com:kcajmagic/goaws.git /tmp/goaws
pushd /tmp/goaws
git checkout adding_http_support
docker build -t goaws:local .
popd

echo "Running services..."
CADUCEUS_VERSION=${CADUCEUS_VERSION:-0.4.2} \
ARGUS_VERSION=${ARGUS_VERSION:-latest} \
TR1D1UM_VERSION=${TR1D1UM_VERSION:-0.5.1} \
HECATE_VERSION=${HECATE_VERSION:-latest} \
docker-compose -f $ROOT_DIR/deploy/docker-compose/docker-compose.yml up -d $@

bash config_dynamodb.sh

