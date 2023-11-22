FROM golang:1.21

# Set the Current Working Directory inside the container
WORKDIR /app/tracing-poc

# Copy everything from the current directory to the PWD (Present Working Directory) inside the container
COPY . .

# Build the Go app
RUN go build -o ./out/tracing-poc .


# This container exposes port 8080 to the outside world
EXPOSE 8081

# Run the executable
CMD ["./out/tracing-poc"]
