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

# Create a tag, update the version in the main.go file and push it to the remote repository
create-version TAG:
    #!/usr/bin/env bash
    TAGVAR={{TAG}}
    TAGTRIM=${TAGVAR#v}
    sed -i '' "s/Version:.*\".*\"/Version:          \"$TAGTRIM\"/" cmd/af/main.go
    git add .
    git commit -m "Update version to {{TAG}}"
    git push origin main
    git tag {{TAG}}
    git push origin {{TAG}}
