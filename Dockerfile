FROM golang:1.25-alpine

WORKDIR /app

# Install dependencies
RUN apk --no-cache add ca-certificates tzdata git

# Copy and build
COPY . .
RUN go mod download
RUN CGO_ENABLED=0 go build -o pekka 

CMD ["./pekka", "start"]