#!/bin/bash
set -e

LAMBDA_NAME="micysql-bridge"
OUTPUT_DIR="../dist"
ENTRY="./main.go"   # Your main function path

mkdir -p $OUTPUT_DIR

echo "📦 Building $LAMBDA_NAME..."
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
  go build -ldflags="-s -w" -o $OUTPUT_DIR/$LAMBDA_NAME .

cd $OUTPUT_DIR
zip $LAMBDA_NAME.zip $LAMBDA_NAME
cd -

