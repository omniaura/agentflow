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

version:
    git fetch --tags && git describe --tags --abbrev=0

create-version TAG:
    #!/usr/bin/env bash
    git tag {{TAG}}
    sed -i '' "s/Version:.*\".*\"/Version:          \"{{TAG}}\"/" cmd/af/main.go
    echo "Updated version in main.go to {{TAG}}"
    git push origin {{TAG}}
