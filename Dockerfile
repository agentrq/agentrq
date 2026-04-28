FROM node:25-alpine3.22 AS frontendbuild

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


FROM golang:1.25-bookworm AS build

WORKDIR /app
RUN apt-get update && \
  apt-get install -y gcc && \
  rm -rf /var/lib/apt/lists/*

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
