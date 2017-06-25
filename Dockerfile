FROM golang:latest

RUN mkdir -p /go/src/app
WORKDIR /go/src/app
ADD . /go/src/app/ 
RUN go get -v -d \
  && CGO_ENABLED=0 GOOS=linux go install -a -installsuffix cgo app

FROM alpine:latest
COPY --from=0 /go/bin/app /app
COPY example-pv.yml \
     example-pvc.yml \
     example-deployment.yml \
     example-service.yml /
CMD ["/app", "80"]
