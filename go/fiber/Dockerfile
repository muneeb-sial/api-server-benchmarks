# Linux build of the server for `bench run -docker`; GOMAXPROCS/GOMEMLIMIT come from bench.json.
FROM golang:1.24-bookworm AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /server .

FROM debian:bookworm-slim
COPY --from=build /server /usr/local/bin/server
CMD ["server"]
