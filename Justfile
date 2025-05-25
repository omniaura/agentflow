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

# Run gofmt with -s (simplify)
fmt:
    gofmt -s -w ./..

# Get the latest tag
version:
    git fetch --tags && git describe --tags --abbrev=0

# Create a tag, update the version in the main.go file, push it to the remote repository and create a GitHub release
create-version TAG:
    #!/usr/bin/env bash
    TAGVAR={{TAG}}
    TAGTRIM=${TAGVAR#v}
    sed -i '' "s/Version:.*\".*\"/Version:          \"$TAGTRIM\"/" cmd/af/main.go
    git add .
    git commit -m "Update version to {{TAG}}"
    git push origin main
    git tag -a {{TAG}} -m "Release {{TAG}}"
    git push origin {{TAG}}
    gh release create {{TAG}} --generate-notes
alias cv := create-version

merge-dependabot:
	./scripts/github/merge_dependabot.sh

lint:
    go fmt ./...
    go vet ./...