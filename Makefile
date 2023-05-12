build:
	go test ./... && \
	CGO_ENABLED=0 \
	GOARCH=amd64 \
	GOOS=linux \
	go build -a -tags netgo -ldflags '-s -w -extldflags "-static"'
