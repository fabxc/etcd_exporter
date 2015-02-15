FROM       golang:1.4
MAINTAINER Fabian Reinartz <fab.reinartz@gmail.com>

RUN     mkdir -p /go/src/etcd_exporter
COPY    . /go/src/etcd_exporter

RUN     go get github.com/tools/godep
WORKDIR /go/src/etcd_exporter
RUN     godep get
RUN     godep go install

EXPOSE     9105
CMD        []
ENTRYPOINT ["etcd_exporter"]