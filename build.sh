#!/bin/bash
set -euo pipefail

# Config
TARGET_DIR="./build"
DIST_DIR="./dist"
DOCKER_CONTEXT="./tmpdocker/context"
DOCKER_OUTPUT="./tmpdocker/out"
MARIADB_VERSION="11.7.2"
MARIADB_GIT_BRANCH="11.7"

# Clean and prepare
rm -rf "$TARGET_DIR" "$DOCKER_CONTEXT" "$DOCKER_OUTPUT"
mkdir -p "$TARGET_DIR/mariadb"
mkdir -p "$DIST_DIR"

# Prepare Docker context to build MariaDB from source
mkdir -p "$DOCKER_CONTEXT" "$DOCKER_OUTPUT"

cat > "$DOCKER_CONTEXT/Dockerfile" <<EOF
FROM amazonlinux:2023

ARG MARIADB_GIT_BRANCH

RUN dnf groupinstall -y "Development Tools" && \
    dnf install -y cmake ncurses-devel openssl-devel libaio-devel \
    boost-devel libevent-devel bison zlib-devel wget git make tar \
    libxcrypt-compat systemd-libs liburing findutils && \
    dnf clean all

WORKDIR /build

RUN mkdir -p /out/bin /out/lib && \
    git clone --depth 1 --branch \${MARIADB_GIT_BRANCH} https://github.com/MariaDB/server mariadb-server && \
    cmake -S mariadb-server -B build \
        -DCMAKE_INSTALL_PREFIX=/out \
        -DWITH_UNIT_TESTS=0 \
        -DWITH_SSL=system \
        -DWITH_ZLIB=system \
        -DWITH_PAM=OFF \
        -DWITH_SYSTEMD=no && \
    cmake --build build --parallel 4 && \
    cmake --install build

# Pre-initialize a clean data directory
RUN mkdir -p /out/micydb && /out/scripts/mariadb-install-db --datadir=/out/micydb

# Capture libraries not present on AL2023 (lambda)
RUN mkdir -p /out/libs/ && ldd /out/bin/mariadbd | grep '=> /' | awk '{print \$3}' | while read -r lib; do cp -v "\$lib" /out/libs/; done
RUN strip /out/bin/mariadbd
RUN dnf install -y bash
ENTRYPOINT ["/bin/bash", "-c"]
CMD ["tar -C /out -czf /host/mariadb-bundle.tar.gz ."]
EOF

# Copy Dockerfile and build
echo "🐳 Building MariaDB from source using Docker..."
docker build -t micysql-mariadb-src --build-arg MARIADB_GIT_BRANCH="$MARIADB_GIT_BRANCH" "$DOCKER_CONTEXT"

echo "📦 Running container to build and export binaries..."
docker run --rm -v "$PWD/$TARGET_DIR/mariadb:/host" micysql-mariadb-src

# Finished with docker, clean up
rm -rf tmpdocker

echo "📦 Extracting MariaDB binaries..."
tar -xf "$PWD/$TARGET_DIR/mariadb/mariadb-bundle.tar.gz" -C "$TARGET_DIR" --strip-components=0

mkdir -p "$TARGET_DIR/lambda/bin/"
mkdir -p "$TARGET_DIR/lambda/lib/"
mkdir -p "$TARGET_DIR/lambda/share/"

echo "🔍 Copying mariadbd and shared libs to Lambda package..."
cp "$TARGET_DIR"/bin/mariadbd "$TARGET_DIR/lambda/bin/"
cp -r "$TARGET_DIR"/libs/* "$TARGET_DIR/lambda/lib/"
cp "$TARGET_DIR"/share/*.sql "$TARGET_DIR/lambda/share/"


echo "🛠️ Building Go Lambda bootstrap (Go 1.24.1)..."
GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o "$TARGET_DIR/lambda/bootstrap" ./cmd/micysql

echo "📦 Packaging micysql.zip for Lambda..."
cd "$TARGET_DIR/lambda"
zip -r9 "$OLDPWD/$DIST_DIR/micysql.zip" .
cd -

echo "✅ Done! Lambda ARM64 package created: $DIST_DIR/micysql.zip"
