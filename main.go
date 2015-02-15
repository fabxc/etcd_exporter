package main

import (
	"flag"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	// TODO: full replacement of this client with the package in etcd is being
	// discussed. For now using it is easier to support 0.4 and 2.0 but it should
	// be swapped eventually.
	"github.com/coreos/go-etcd/etcd"
)

var (
	etcdAddress     = flag.String("etcd.address", "http://127.0.0.1:4001", "Address of the initial etcd instance.")
	etcdTimeout     = flag.Duration("haproxy.timeout", DefaultTimeout, "Timeout for scraping an etcd member.")
	refreshInterval = flag.Duration("web.refresh", DefaultRefreshInterval, "Refresh interval to sync machines with the etcd cluster.")
	listenAddress   = flag.String("web.listen-address", ":9105", "Address to listen on for web interface and telemetry.")
	metricsPath     = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
)

const (
	DefaultTimeout         = 5 * time.Second
	DefaultRefreshInterval = 10 * time.Second
)

type etcdCollectors map[string]prometheus.Collector

// refresh registers/unregisters exporters based on the given
// set of machines of the cluster.
func (ec etcdCollectors) refresh(machines []string) {
	// TODO: as of etcd v2.0 members may advertise multiple peer addresses so
	// deduping by host:port is not sufficient as we might scrape the same member
	// twice.
	// Deduping should be done by member ID which is available at /v2/members in v2.0.
	// Should be lined up with switching to etcd/client.
	newMachines := map[string]*url.URL{}
	for _, nm := range machines {
		u, err := url.Parse(nm)
		if err != nil {
			log.Printf("error parsing machine url %s: %s", nm, err)
			continue
		}
		if _, ok := newMachines[u.Host]; !ok || u.Scheme == "https" {
			newMachines[u.Host] = u
		}
	}

	for m, c := range ec {
		if _, ok := newMachines[m]; !ok {
			log.Printf("machine %s left, stopping exporting", m)
			prometheus.Unregister(c)
			delete(ec, m)
		} else {
			delete(newMachines, m)
		}
	}

	for nm, u := range newMachines {
		log.Printf("machine %s joined, start exporting", nm)
		e := NewExporter(u.String(), *etcdTimeout)
		prometheus.MustRegister(e)
		ec[nm] = e
	}
}

func main() {
	flag.Parse()

	c := etcd.NewClient([]string{*etcdAddress})
	collectors := etcdCollectors{}

	log.Println("start exporting etcd from", *etcdAddress)

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
