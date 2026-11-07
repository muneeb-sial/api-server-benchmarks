# Linux build of the server for `bench run -docker`; same release profile as Cargo.toml.
FROM rust:1-slim-bookworm AS build
WORKDIR /app
COPY Cargo.toml Cargo.lock ./
COPY src src
RUN cargo build --release --locked

FROM debian:bookworm-slim
COPY --from=build /app/target/release/axum-bench /usr/local/bin/server
CMD ["server"]
