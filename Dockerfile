FROM golang:1.19 as gobuild

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o /capd-eks-registrar main.go

RUN curl -sLO "https://github.com/weaveworks/eksctl/releases/latest/download/eksctl_Linux_amd64.tar.gz"

RUN tar -xzf eksctl_Linux_amd64.tar.gz -C /usr/local/bin && \
    rm eksctl_Linux_amd64.tar.gz

RUN curl -LO https://dl.k8s.io/release/v1.27.2/bin/linux/amd64/kubectl && \
    install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl

FROM alpine

COPY --from=gobuild /capd-eks-registrar /capd-eks-registrar
COPY --from=gobuild /usr/local/bin/eksctl /usr/local/bin/eksctl
COPY --from=gobuild /usr/local/bin/kubectl /usr/local/bin/kubectl

# Run the executable
CMD ["/capd-eks-registrar"]