FROM golang:1.25.5-alpine

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o /usr/local/bin/casper-mcp ./cmd/casper-mcp

ENV CASPER_MCP_BIN=/usr/local/bin/casper-mcp

CMD ["go", "test", "-tags=e2e", "-v", "-count=1", "./tests/e2e/..."]
