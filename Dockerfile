# deck — a kanban TUI for dstask. Run interactively against a mounted ~/.dstask store:
#
#   docker build -t deck .
#   docker run --rm -it \
#     -v "$HOME/.dstask:/root/.dstask" \
#     -v "$HOME/.gitconfig:/root/.gitconfig:ro" \   # commit identity for writes
#     deck
#
# It's a TUI, so -it (a TTY) is required. Hooks (enter/e/I/:) are host tools, so in a
# container deck runs as the plain standalone board. Mount ~/.config/deck/config.toml
# (ro) to theme it or change columns.

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /deck ./cmd/deck

FROM alpine:3.21
# git: dstask commits each change via the git CLI. The --system identity is a fallback;
# a mounted ~/.gitconfig (--global) or the store's own .git/config overrides it.
RUN apk add --no-cache git ca-certificates \
 && git config --system user.name deck \
 && git config --system user.email deck@localhost
COPY --from=build /deck /usr/local/bin/deck
WORKDIR /root
ENTRYPOINT ["deck"]
