FROM debian:bullseye-slim as main

RUN apt-get update -y
RUN apt-get install -y ca-certificates curl

ENV TZ=UTC

FROM golang:1.20.4-bullseye as build

RUN apt-get install gcc libc6-dev

#RUN wget -qO - https://packages.confluent.io/deb/7.2/archive.key | apt-key add -
#RUN echo "deb https://packages.confluent.io/deb/7.2 stable main"  > /etc/apt/sources.list.d/backports.list
#RUN echo "deb https://packages.confluent.io/clients/deb buster main" > /etc/apt/sources.list.d/backports.list
#RUN apt-get update
#RUN apt-get install -y librdkafka1 librdkafka-dev

RUN mkdir /app
WORKDIR /app

RUN mkdir jitsubase
RUN mkdir bulkerlib
RUN mkdir bulkerapp

COPY jitsubase/go.mod ./jitsubase/
COPY jitsubase/go.sum ./jitsubase/
COPY bulkerlib/go.mod ./bulkerlib/
COPY bulkerlib/go.sum ./bulkerlib/
COPY bulkerapp/go.mod ./bulkerapp/
COPY bulkerapp/go.sum ./bulkerapp/

RUN go work init jitsubase bulkerlib bulkerapp

WORKDIR /app/bulkerapp

RUN go mod download

WORKDIR /app

COPY .. .

# Build bulker
RUN go build -o bulkerapp ./bulkerapp

#######################################
# FINAL STAGE
FROM main as final

RUN mkdir /app
WORKDIR /app

# Copy bulkerapp
COPY --from=build /app/bulkerapp/bulkerapp ./bulkerapp
#COPY ./config.yaml ./

CMD ["/app/bulkerapp"]