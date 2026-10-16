# ================================================================
# doc-ai-gateway build image
# 纯 Go 编译（无 CGO），只需要 Go toolchain
# ================================================================
FROM golang:1.24-alpine AS base

ENV GOPROXY=https://goproxy.cn,direct
ENV CGO_ENABLED=0

WORKDIR /workspace