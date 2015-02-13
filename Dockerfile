FROM google/golang

WORKDIR /gopath/src/github.com/fabxc/etcd_exporter
ADD . /gopath/src/github.com/fabxc/etcd_exporter
RUN go get github.com/fabxc/etcd_exporter
EXPOSE 9105

CMD []
ENTRYPOINT ["/gopath/bin/etcd_exporter"]