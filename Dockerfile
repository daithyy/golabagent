FROM golang:1.27-alpine AS build

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /golabagent

FROM alpine:3

COPY --from=build /golabagent /golabagent
COPY data /data

CMD ["/golabagent"]
