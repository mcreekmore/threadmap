# Only the last stage is architecture-specific: the first two run natively on the
# build machine, so publishing the arm64 image doesn't need emulation.
FROM --platform=$BUILDPLATFORM node:24-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ .
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS build
ARG TARGETARCH
WORKDIR /src
COPY go.mod *.go ./
COPY --from=web /web/dist web/dist
RUN CGO_ENABLED=0 GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /threadmap .

FROM scratch
COPY --from=build /threadmap /threadmap
ENTRYPOINT ["/threadmap"]
