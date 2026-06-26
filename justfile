# deck — task runner (https://github.com/casey/just). Run `just` for the default build.
prefix := env_var_or_default("PREFIX", env_var("HOME") / ".local")

# build the binary into the repo
build:
    go build -o deck ./cmd/deck

# build + install to $PREFIX/bin (default ~/.local/bin)
install:
    go build -o {{ prefix }}/bin/deck ./cmd/deck

# build, then run with any extra args (e.g. `just run --once`)
run *args: build
    ./deck {{ args }}

# format · vet · test — the pre-commit sweep
check: fmt vet test

fmt:
    gofmt -w .

vet:
    go vet ./...

test:
    go test ./...

# update go.mod/go.sum
tidy:
    go mod tidy

clean:
    rm -f deck

# build the docker image (tag: deck)
image:
    docker build -t deck .

# run the image against your ~/.dstask store (interactive). Add a gitconfig mount
# (-v "$HOME/.gitconfig:/root/.gitconfig:ro") for real commit authorship.
docker-run: image
    docker run --rm -it -v "$HOME/.dstask:/root/.dstask" deck
