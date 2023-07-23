FROM golang:1.20 as builder
ARG TARGETOS
ARG TARGETARCH=amd64

WORKDIR /workspace

COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY main.go main.go
COPY internal/ internal/
COPY pkg/ pkg/

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -o manager main.go

ENTRYPOINT ["/workspace/manager"]
# FROM gcr.io/distroless/static:nonroot
# WORKDIR /
# COPY --from=builder /workspace/manager .
# USER 65532:65532
# ENTRYPOINT ["/manager"]
