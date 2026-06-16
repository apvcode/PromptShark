# Stage 1: Build C++ Core Engine
FROM debian:bullseye-slim AS cpp-builder
RUN apt-get update && apt-get install -y cmake g++ make
WORKDIR /app
COPY CMakeLists.txt ./
COPY core/ ./core/
RUN mkdir build && cd build && cmake .. && make

# Stage 2: Build Go Proxy
FROM golang:1.22-bullseye AS go-builder
WORKDIR /app
# Pre-download dependencies to optimize caching
COPY go.mod go.sum ./
RUN go mod download
# Copy the rest and build
COPY proxy/ ./proxy/
WORKDIR /app/proxy
# CGO_ENABLED=1 is required for SQLite
RUN CGO_ENABLED=1 go build -o agent_supervisor .

# Stage 3: Final Runtime Image
FROM debian:bullseye-slim
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /app
# Copy compiled C++ engine
COPY --from=cpp-builder /app/build/core_engine ./build/
# Copy the configuration file (C++ engine expects it in ../ relative to proxy)
COPY loop_config.txt ./

# Setup DB directory and schema
RUN mkdir -p ./db
COPY db/schema.sql ./db/

# Copy Go binary
COPY --from=go-builder /app/proxy/agent_supervisor ./proxy/

# Volume for persisting SQLite DB
VOLUME ["/app/db"]

# Run from proxy directory just like in local development
WORKDIR /app/proxy
EXPOSE 8080

CMD ["./agent_supervisor"]
