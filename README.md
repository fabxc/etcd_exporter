# etcd_exporter

This is a simple server that frequently scrapes statistics from all members of an etcd cluster
and exposes them for to be scraped by Prometheus.

[![Build Status](https://travis-ci.org/fabxc/etcd_exporter.png?branch=master)](https://travis-ci.org/fabxc/etcd_exporter) etcd 2.0.x
[![Build Status](https://travis-ci.org/fabxc/etcd_exporter.png?branch=etcd-v0.4)](https://travis-ci.org/fabxc/etcd_exporter) etcd 0.4.x

[![Docker Repository on Quay.io](https://quay.io/repository/coreos/etcd-git/status "Docker Repository on Quay.io")](https://quay.io/repository/coreos/etcd-git)

```
godep get && godep go build
./etcd_exporter
```

## etcd versions

There are slight differences in the exposed statistics (and version of the used etcd client)
between etcd version 0.4.x and 2.0.x.
For clusters using etcd 0.4 the etcd-v0.4 branch can be used.

## Docker

To run the etcd_exporter in a docker container run

```
docker run -p 9105:9105 quay.io/fabxc/etcd_exporter -etcd.address="http://some.member:4001"
```

For clusters using etcd 0.4.x

```
docker run -p 9105:9105 quay.io/fabxc/etcd_exporter:etcd-v0.4 -etcd.address="http://some.member:4001"
```