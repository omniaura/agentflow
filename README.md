# agentflow
[![Godoc Reference](https://godoc.org/github.com/omniaura/agentflow?status.svg)](http://godoc.org/github.com/omniaura/agentflow)
[![Go Coverage](https://github.com/omniaura/agentflow/wiki/coverage.svg)](https://raw.githack.com/wiki/omniaura/agentflow/coverage.html)
[![Go Report Card](https://goreportcard.com/badge/github.com/omniaura/agentflow)](https://goreportcard.com/report/github.com/omniaura/agentflow)

## Installation

    go install github.com/omniaura/agentflow/cmd/af@latest

As of go 1.24 you can now use `af` as a go tool:

    go get -tool github.com/omniaura/agentflow/cmd/af@latest

This installs `af` scoped to your go project and you call it using `go tool af`

## Usage

    af gen prompts examples/simple
