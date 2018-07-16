FROM golang:alpine as builder
ENV GOBIN /go/bin

RUN mkdir /app
RUN mkdir /go/src/app

WORKDIR /go/src/app

RUN apk add --no-cache git
RUN go get -u github.com/golang/dep/...

COPY . /go/src/app
RUN dep ensure
RUN go build -o /app/main

FROM alpine
ARG DB_HOST
ARG PORT
ARG NSQD_TCP_URL
ARG NSQLOOKUP_HTTP_URL

ENV DB_HOST=$DB_HOST
ENV PORT=$PORT
ENV NSQD_TCP_URL=$NSQD_TCP_URL
ENV NSQLOOKUP_HTTP_URL=$NSQLOOKUP_HTTP_URL

EXPOSE $PORT

WORKDIR /app
COPY --from=builder /app/main /app/

ENTRYPOINT ["/app/main"]