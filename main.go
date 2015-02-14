package main

import (
	"flag"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	// TODO: full replacement of this client with the package in etcd is being
	// discussed. For now using it is easier to support 0.4 and 2.0 but it should
	// be swapped eventually.
	"github.com/coreos/go-etcd/etcd"
)

var (
	etcdAddress     = flag.String("etcd.address", "http://127.0.0.1:4001", "Address of the initial etcd instance.")
	refreshInterval = flag.Duration("etcd.refresh", DefaultRefreshInterval, "Refresh interval to sync machines with the etcd cluster.")
	listenAddress   = flag.String("web.listen-address", ":9105", "Address to listen on for web interface and telemetry.")
	metricsPath     = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
)

const DefaultRefreshInterval = 10 * time.Second

type etcdCollectors map[string]prometheus.Collector

// refresh registers/unregisters exporters based on the given
// set of machines of the cluster.
func (ec etcdCollectors) refresh(machines []string) {
	newMachines := map[string]string{}
	for _, nm := range machines {
		p := strings.SplitAfter(nm, ":")
		newMachines[p[0]+p[1]] = p[2]
	}

	for m, c := range ec {
		if _, ok := newMachines[m]; !ok {
			prometheus.Unregister(c)
			delete(ec, m)
		} else {
			delete(newMachines, m)
		}
	}

	for nm, port := range newMachines {
		e := NewExporter(nm + port)
		prometheus.MustRegister(e)
		ec[nm] = e
	}
}

func main() {
	flag.Parse()

	c := etcd.NewClient([]string{*etcdAddress})
	collectors := etcdCollectors{}

	go func() {
		for {
			if c.SyncCluster() {
				collectors.refresh(c.GetCluster())
			}
			<-time.After(*refreshInterval)
		}
	}()

	http.Handle(*metricsPath, prometheus.Handler())
	err := http.ListenAndServe(*listenAddress, nil)
	if err != nil {
		log.Fatal(err)
	}
}
