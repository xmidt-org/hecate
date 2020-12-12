# hecate

(pronounced "HEK-uh-tee")

Hecate was a goddess in ancient Greek religion associated with crossroads. In XMiDT, Hecate is a tool to help transition webhook backends in XMiDT from AWS SNS to Argus.

[![Build Status](https://github.com/xmidt-org/hecate/workflows/CI/badge.svg)](https://github.com/xmidt-org/hecate/actions)
[![codecov.io](http://codecov.io/github/xmidt-org/hecate/coverage.svg?branch=main)](http://codecov.io/github/xmidt-org/hecate?branch=main)
[![Go Report Card](https://goreportcard.com/badge/github.com/xmidt-org/hecate)](https://goreportcard.com/report/github.com/xmidt-org/hecate)
[![Apache V2 License](http://img.shields.io/badge/license-Apache%20V2-blue.svg)](https://github.com/xmidt-org/hecate/blob/main/LICENSE)
[![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=xmidt-org_hecate&metric=alert_status)](https://sonarcloud.io/dashboard?id=xmidt-org_PROJECT)
[![GitHub release](https://img.shields.io/github/release/xmidt-org/hecate.svg)](CHANGELOG.md)
[![GoDoc](https://godoc.org/github.com/xmidt-org/hecate?status.svg)](https://godoc.org/github.com/xmidt-org/hecate)

## Summary

XMiDT has historically relied on SNS as the method to keep a distributed list of event webhook subscriptions. Now, it relies on [argus](https://github.com/xmidt-org/argus/) to handle those storage needs. As teams may need to upgrade their XMiDT services without downtime or disruptions, Hecate is here to help ensure all the webhook data between SNS and Argus is synchronized during the migration.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Details](#details)
- [Build](#build)
- [Contributing](#contributing)

## Code of Conduct

This project and everyone participating in it are governed by the [XMiDT Code Of Conduct](https://xmidt.io/code_of_conduct/).
By participating, you agree to this Code.

## Details

Hecate's job consists in listening for webhook updates from SNS and pushing them to Argus.

## Build

### Source

In order to build from source, you need a working 1.x Go environment.
Find more information on the [Go website](https://golang.org/doc/install).

Then, clone the repository and build using make:

```bash
git clone git@github.com:xmidt-org/hecate.git
cd hecate
make build
```

### Makefile

The Makefile has the following options you may find helpful:

- `make build`: builds the Hecate binary
- `make docker`: fetches all dependencies from source and builds a Hecate docker image
- `make local-docker`: vendors dependencies and builds a Hecate docker image (recommended for local testing)
- `make test`: runs unit tests with coverage for Hecate
- `make clean`: deletes previously-built binaries and object files

### Docker

The docker image can be built either with the Makefile or by running a docker
command.  Either option requires first getting the source code.

See [Makefile](#Makefile) on specifics of how to build the image that way.

If you'd like to build it without make, follow these instructions based on your use case:

- Local testing

```bash
go mod vendor
docker build -t hecate:local -f deploy/Dockerfile .
```

This allows you to test local changes to a dependency. For example, you can build
a hecate image with the changes to an upcoming changes to [webpa-common](https://github.com/xmidt-org/webpa-common) by using the [replace](https://golang.org/ref/mod#go) directive in your go.mod file like so:

```go.mod
replace github.com/xmidt-org/webpa-common v1.10.2 => ../webpa-common
```

**Note:** if you omit `go mod vendor`, your build will fail as the path `../webpa-common` does not exist on the builder container.

- Building a specific version

```bash
git checkout v0.5.1
docker build -t hecate:v0.5.1 -f deploy/Dockerfile .
```

## Contributing

Refer to [CONTRIBUTING.md](CONTRIBUTING.md).
