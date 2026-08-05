# Build the single embedded binary, then ship it on a minimal base.
#
# NOTE ON THE APP EMBED (read before relying on this image):
# backend/cmd/pango embeds both the marketing/docs site (site/ -> served at
# /site/, see backend/cmd/pango/site_embed.go) and the built React app
# (web/dist/ -> served at "/", see backend/cmd/pango/app_embed.go), both
# behind the `embed_frontend` build tag. The frontend project lives under
# web/ (its own package.json, src/, ...) alongside the backend/ Go module.
FROM node:20-bookworm AS frontend
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ .
RUN npm run build

FROM golang:1.25-bookworm AS backend
WORKDIR /app
COPY backend/go.mod backend/go.sum ./backend/
RUN cd backend && go mod download
COPY . .
# Marketing site, embedded today (see site_embed.go).
RUN rm -rf backend/cmd/pango/site && cp -R site backend/cmd/pango/site
# Built app, embedded today (see app_embed.go).
COPY --from=frontend /app/web/dist ./backend/cmd/pango/dist
ARG VERSION=docker
RUN cd backend && CGO_ENABLED=0 go build -tags embed_frontend \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /pango ./cmd/pango

FROM gcr.io/distroless/static-debian12
COPY --from=backend /pango /pango
# pango does not read these from the environment today — see backend's
# main.go, which only accepts -addr/-db/-demo/-origins/-secure-cookies flags.
# They are declared here to match the house Dockerfile convention and to
# document the values this image's default CMD wires into flags below; a
# deployment needing different values must override CMD directly, e.g.:
#   docker run ... ghcr.io/vul-os/pango:latest -addr=0.0.0.0:9000 -db=/data/pango.db
ENV PANGO_HOST=0.0.0.0 \
    PANGO_PORT=8099 \
    PANGO_DATA_DIR=/data
VOLUME ["/data"]
EXPOSE 8099
ENTRYPOINT ["/pango"]
CMD ["-addr=0.0.0.0:8099", "-db=/data/pango.db"]
