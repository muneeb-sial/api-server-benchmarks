# Linux build of the server for `bench run -docker`: GCC release build, Drogon from source.
#
# Versions match the native build, which takes Drogon and trantor from vcpkg at the commit
# pinned in build.cmd (VCPKG_COMMIT=9a5dfef5d1a0438a7d24856a85bd60f309d355e9):
#   ports/drogon   1.9.13 (port-version 2)   ports/trantor  1.5.28
# Of vcpkg's patches only drogon's 0005 changes runtime behaviour; it is vendored here as
# drogon-chunked-encoding.patch. The rest only adapt the build to vcpkg (trantor's
# disable-werror patch is replayed with sed). Like vcpkg, trantor uses c-ares and Drogon
# uses brotli. Bump these together with VCPKG_COMMIT.
FROM debian:bookworm AS build
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates git cmake ninja-build g++ libjsoncpp-dev uuid-dev zlib1g-dev libssl-dev \
      libc-ares-dev libbrotli-dev \
 && rm -rf /var/lib/apt/lists/*
ARG TRANTOR_VERSION=v1.5.28
ARG DROGON_VERSION=v1.9.13
RUN git clone --depth 1 --branch ${TRANTOR_VERSION} https://github.com/an-tao/trantor /trantor \
 && sed -i 's/ -Werror)/)/' /trantor/CMakeLists.txt \
 && cmake -S /trantor -B /trantor/build -G Ninja -DCMAKE_BUILD_TYPE=Release -DBUILD_TESTING=OFF \
 && cmake --build /trantor/build && cmake --install /trantor/build
COPY drogon-chunked-encoding.patch /tmp/
RUN git clone --depth 1 --branch ${DROGON_VERSION} https://github.com/drogonframework/drogon /drogon \
 && git -C /drogon apply /tmp/drogon-chunked-encoding.patch \
 && cmake -S /drogon -B /drogon/build -G Ninja -DCMAKE_BUILD_TYPE=Release -DUSE_SUBMODULE=OFF \
      -DBUILD_CTL=OFF -DBUILD_EXAMPLES=OFF -DBUILD_ORM=OFF -DBUILD_TESTING=OFF \
 && cmake --build /drogon/build && cmake --install /drogon/build
WORKDIR /app
COPY CMakeLists.txt main.cc ./
RUN cmake -S . -B build -G Ninja -DCMAKE_BUILD_TYPE=Release && cmake --build build

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      libjsoncpp25 libssl3 zlib1g libc-ares2 libbrotli1 \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /app/build/drogon-bench /usr/local/bin/server
CMD ["server"]
