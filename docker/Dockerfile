# Builds the ghp CLI from the upstream GHP-GoLang-Framework/ghp repo, then
# uses it to transpile and compile this project into a single static
# binary. Standalone build: no local sibling checkout of ghp required.
FROM golang:1.27-alpine AS ghp-builder
ENV CGO_ENABLED=0
RUN apk add --no-cache git
WORKDIR /src
RUN git clone --depth 1 https://github.com/GHP-GoLang-Framework/ghp.git .
RUN go build -o /usr/local/bin/ghp ./src/cmd

FROM golang:1.27-alpine AS app-builder
ENV CGO_ENABLED=0
COPY --from=ghp-builder /usr/local/bin/ghp /usr/local/bin/ghp
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN ghp build .

FROM scratch
COPY --from=app-builder /app/app /app
ENTRYPOINT ["/app"]
