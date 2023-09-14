FROM docker.io/library/golang:1.20 as builder

WORKDIR /build

# Cache modules
COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . .
RUN make build

FROM busybox
COPY --from=builder /build/grafana-organizations-operator /
COPY --from=alpine:latest /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY dashboards/ /dashboards/
CMD ["/grafana-organizations-operator"]
