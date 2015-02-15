FROM       google/golang
MAINTAINER Fabian Reinartz <fab.reinartz@gmail.com>

WORKDIR /gopath/src/github.com/fabxc/etcd_exporter
ADD     . /gopath/src/github.com/fabxc/etcd_exporter
RUN     go get github.com/fabxc/etcd_exporter

EXPOSE     9105
ENTRYPOINT ["etcd_exporter"]