FROM golang:1.17-alpine as builder

RUN apk --update --no-cache add make git protoc coreutils curl tzdata

# ENV TZ Europe/London

WORKDIR /go/src/github.com/pechorka/whattobuy
COPY . .

RUN make build

# Second stage
FROM alpine

WORKDIR /bot

COPY --from=builder /go/src/github.com/pechorka/whattobuy/bin/bot wtbbot
COPY --from=builder /go/src/github.com/pechorka/whattobuy/var/cert cert

CMD ["./wtbbot"]
