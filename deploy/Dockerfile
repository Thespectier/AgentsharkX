# syntax=docker/dockerfile:1@sha256:87999aa3d42bdc6bea60565083ee17e86d1f3339802f543c0d03998580f9cb89

FROM node:24.13.0-alpine3.23@sha256:cd6fb7efa6490f039f3471a189214d5f548c11df1ff9e5b181aa49e22c14383e AS web-build
WORKDIR /src/apps/web
COPY apps/web/package.json apps/web/package-lock.json ./
RUN npm ci
COPY apps/web/ ./
ENV VITE_ENABLE_MOCKS=false
RUN npm run build

FROM golang:1.26.5-alpine3.23@sha256:622e56dbc11a8cfe87cafa2331e9a201877271cbff918af53d3be315f3da88cc AS server-build
WORKDIR /src/apps/server
COPY apps/server/go.mod ./
COPY apps/server/ ./
RUN find internal/web/static -mindepth 1 -delete
COPY --from=web-build /src/apps/web/dist/ ./internal/web/static/
ARG AGENTSHARK_VERSION=0.7.0-preview
ARG AGENTSHARK_REVISION=unknown
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X main.version=${AGENTSHARK_VERSION} -X main.revision=${AGENTSHARK_REVISION}" \
    -o /out/agentshark ./cmd/agentshark

FROM alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659
RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 65532 agentshark \
    && adduser -S -D -H -u 65532 -G agentshark agentshark
COPY --from=server-build /out/agentshark /usr/local/bin/agentshark
USER 65532:65532
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=6 \
  CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/agentshark"]
