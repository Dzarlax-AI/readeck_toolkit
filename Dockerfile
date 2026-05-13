# syntax=docker/dockerfile:1
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/readeck-bot ./cmd/bot
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/readeck-mcp ./cmd/mcp

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/readeck-bot /usr/local/bin/readeck-bot
COPY --from=build /out/readeck-mcp /usr/local/bin/readeck-mcp
USER nonroot
# default command — override in compose for the bot variant
ENTRYPOINT []
CMD ["readeck-mcp"]
