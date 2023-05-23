# Use the official Golang image to create a build artifact.
# This is based on Debian and sets the GOPATH to /go.
FROM golang:latest as builder

LABEL maintainer="Lyndon L <lyndon@zug.dev>"


ARG TARGETOS
ARG TARGETARCH

# Create and change to the app directory.
WORKDIR /app

# Retrieve application dependencies using go modules.
# Allows container builds to reuse downloaded dependencies.
COPY go.mod .
COPY go.sum .
COPY main.go .

COPY public ./public
COPY templates ./templates
COPY .well-known ./.well-known

RUN go mod download

# Build the binary.
# -mod=readonly ensures immutable go.mod and go.sum in container builds.
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -mod=readonly -v -o main

# Use the official Alpine image for a lean production container.
# https://hub.docker.com/_/alpine
# https://docs.docker.com/develop/develop-images/multistage-build/#use-multi-stage-builds
FROM alpine:3
RUN apk add --no-cache ca-certificates

# Copy the binary to the production image from the builder stage.
COPY --from=builder /app/main /main
COPY --from=builder /app/public /public
COPY --from=builder /app/templates /templates
COPY --from=builder /app/.well-known /.well-known 


# Run the web service on container startup.
CMD ["/main"]