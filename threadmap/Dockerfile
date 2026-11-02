FROM golang:1.23-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /threadmap .

FROM scratch
COPY --from=build /threadmap /threadmap
ENTRYPOINT ["/threadmap"]
