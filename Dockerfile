# FROM --platform=${BUILDPLATFORM} docker.io/library/node:18.19.0-alpine3.18@sha256:4bdb3f3105718f0742bc8d64bb4e36e8f955ebbee295325e40ae80bc8ef78833 as stage1
FROM node:18.16.0 as stage1
# RUN apk add bash

WORKDIR /app

COPY package.json package.json
COPY package-lock.json package-lock.json
COPY scripts/ scripts/
COPY patches/ patches/

# # TARGETOS is an automatic platform ARG enabled by Docker BuildKit.
# ARG TARGETOS
# # TARGETARCH is an automatic platform ARG enabled by Docker BuildKit.
# ARG TARGETARCH
# RUN npm --target_arch=${TARGETARCH} install
RUN npm install

COPY . .

ARG NODE_ENV=production
RUN npm run build

FROM docker.io/nginxinc/nginx-unprivileged:1.25.3-alpine3.18-slim@sha256:57a630cf4a357007959cac5b8a6d91ff381a55699b31545792fa9b88b26c5f5c
USER root
RUN apk upgrade --no-cache
RUN rm /usr/share/nginx/html/*
USER 101
# COPY --from=stage1 /app/server/public /app

# Nginx serves what is in the /usr/share/nginx/html folder on port 8080.
# when this is deployed in Kubernetes.
COPY --from=stage1 /app/server/public /usr/share/nginx/html
