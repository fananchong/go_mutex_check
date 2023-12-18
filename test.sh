#!/bin/bash

set -ex

go build
mv -f go_mutex_check test
cd test
./go_mutex_check --path=.
cd ..
