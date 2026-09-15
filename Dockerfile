# Pinned to the BUILD platform, not the target, and that is the whole point of
# this line. Under `buildx --platform linux/arm64` on an amd64 runner, an
# unpinned stage runs the entire npm install and vite build under QEMU — and
# Node's V8 JIT emits instructions QEMU's user-mode emulation does not
# implement, so the build dies with
#   qemu: uncaught target signal 4 (Illegal instruction) - core dumped
#
# Emulating it bought nothing even when it worked: the output is JavaScript and
# CSS, byte-identical on either architecture. The platform-specific things npm
# installs here (esbuild, rollup, sharp) stay in this stage and are never
# copied into the image.
FROM --platform=$BUILDPLATFORM node:25-alpine3.22 AS frontendbuild

# Set the working directory inside the container
WORKDIR /app

# Copy package.json
COPY ./frontend/package.json ./

# Install project dependencies
RUN npm install

# Copy the rest of the application code
COPY ./frontend .

# Build the front-end app to copy to public
RUN npm run build

RUN apk add brotli

# Compress files in the root directory (public/)
RUN	find dist -maxdepth 1 -type f -not -name "*.gz" -not -name "*.br" -exec gzip -9 -f -k {} +
RUN find dist -maxdepth 1 -type f -not -name "*.gz" -not -name "*.br" -exec brotli -9 -f -k {} +

# Compress files in the assets directory (public/assets/)
RUN find dist/assets -maxdepth 1 -type f -not -name "*.gz" -not -name "*.br" -exec gzip -9 -f -k {} +
RUN	find dist/assets -maxdepth 1 -type f -not -name "*.gz" -not -name "*.br" -exec brotli -9 -f -k {} +


# Also the build platform: this stage already cross-compiles by setting GOARCH
# from TARGETARCH with CGO disabled, so running the Go toolchain itself under
# emulation was pure cost. Go does not JIT, so this never crashed the way the
# Node stage did — it was just slow.
FROM --platform=$BUILDPLATFORM golang:1.27-bookworm AS build

WORKDIR /app
RUN apt-get update && \
  apt-get install -y gcc && \
  rm -rf /var/lib/apt/lists/*

# The daemon module first: backend/go.mod requires it and replaces it with a
# relative path, because the wire protocol has exactly one definition and both
# sides import it rather than keeping a copy each. The backend is flattened
# into /app here, so ../daemon is /daemon — put it there before anything tries
# to resolve the module graph.
COPY daemon /daemon

COPY backend/go.mod backend/go.sum ./
RUN GO111MODULE=on go mod download

COPY backend/internal internal
COPY backend/cmd cmd
COPY backend/cmd/server/_config _config

RUN rm -f cmd/server/.env*
RUN rm -f cmd/server/_storage/*.db
RUN rm -rf cmd/server/scripts
RUN mkdir -p cmd/server/public
RUN rm -rf cmd/server/public/*

# Copy new public files
COPY --from=frontendbuild /app/dist cmd/server/public

ARG TARGETOS
ARG TARGETARCH
ENV GOOS=${TARGETOS}
ENV GOARCH=${TARGETARCH}
# Using CGO_ENABLED=0 since we are using the pure Go SQLite driver
ENV CGO_ENABLED=0

# Compile statically so we can safely use the scratch image
RUN go build -v -ldflags="-w -s" -o agentrq cmd/server/main.go

# Create a "nobody" non-root user
RUN echo "nobody:x:65534:65534:Nobody:/:" > /etc_passwd


FROM scratch

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /app/agentrq /
COPY --from=build /etc_passwd /etc/passwd
COPY --from=build /app/cmd/server/public /public
COPY --from=build /app/_config /_config

USER nobody

CMD ["/agentrq"]
