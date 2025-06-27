set windows-shell := ["powershell.exe", "-NoLogo", "-Command"]

# Run the agentflow for development
go *ARGS:
    go run cmd/af/main.go {{ARGS}}

# Run the tests
test *ARGS:
    go test ./... {{ARGS}}

# Install the agentflow binary
install:
    cd cmd/af && go install .

fmt:
    gofmt -s -w ./..
    go vet ./...

# Get the latest tag
@version:
    git fetch --tags && git describe --tags --abbrev=0
alias v := version

# Set the version in cfg/cfg.go and update tests, but do not commit or push
set-version-nocommit TAG:
    #!/usr/bin/env bash
    set -e
    TAGVAR={{TAG}}
    TAGTRIM=${TAGVAR#v}
    sed -i '' "s/var Version = \".*\"/var Version = \"$TAGTRIM\"/" cfg/cfg.go
    go test ./pkg/gen/gogen -update

# Set the version, commit, and push
set-version TAG:
    just set-version-nocommit {{TAG}}
    git add cfg/cfg.go pkg/gen/gogen/testdata/*
    git commit -m "Update version to {{TAG}}"
    git push origin main

# Tag the current commit, push the tag, and create a GitHub release
tag-release TAG:
    git tag -a {{TAG}} -m "Release {{TAG}}"
    git push origin {{TAG}}
    gh release create {{TAG}} --generate-notes

# Create a tag, update the version in the main.go file, push it to the remote repository and create a GitHub release
create-version TAG:
    just set-version {{TAG}}
    just tag-release {{TAG}}
alias cv := create-version

merge-dependabot:
	./scripts/github/merge_dependabot.sh

lint:
    go fmt ./...
    go vet ./...

generate:
    go generate ./...