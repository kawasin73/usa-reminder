FROM golang:1.10 as build

RUN go get -t github.com/golang/dep/cmd/dep

WORKDIR /go/src/github.com/kawasin73/usa-reminder

COPY Gopkg.toml Gopkg.lock ./

RUN dep ensure --vendor-only

COPY . ./

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /app .

FROM alpine:latest

LABEL maintainer="kawasin73@gmail.com"

RUN apk add --no-cache ca-certificates

ENV PORT=3000

EXPOSE 3000

COPY --from=build /app /app

CMD ["/app"]
