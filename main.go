package main

import (
	// "encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	etcdAddress   = flag.String("etcd.address", "http://127.0.0.1:4001", "Address of the initial etcd instance.")
	listenAddress = flag.String("web.listen-address", ":9105", "Address to listen on for web interface and telemetry.")
	metricsPath   = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")

	collectors = map[string]prometheus.Collector{}
)

const (
	refreshIntervalSeconds = 10
)

func main() {
	flag.Parse()

	go func() {
		machines := []string{*etcdAddress}
		for {
			machines = getMachines(machines)
			log.Println(machines)
			refreshCollectors(machines)
			<-time.After(refreshIntervalSeconds * time.Second)
		}
	}()

	http.Handle(*metricsPath, prometheus.Handler())
	log.Println("listening on", *listenAddress)
	err := http.ListenAndServe(*listenAddress, nil)
	if err != nil {
		log.Fatal(err)
	}
}

func getMachines(cm []string) []string {

	newMachines := make(map[string]string)

	for _, addr := range cm {
		res, err := http.Get(addr + "/v2/machines")
		if err != nil {
			log.Fatal(err)
		}
		clientsStr, err := ioutil.ReadAll(res.Body)
		res.Body.Close()

		parts := strings.Split(string(clientsStr), ", ")
		for _, p := range parts {
			mp := strings.Split(p, ":")
			newMachines[mp[0]+":"+mp[1]] = mp[2]
		}
	}
	nm := []string{}
	for k, v := range newMachines {
		nm = append(nm, k+":"+v)
	}
	return nm
}

func refreshCollectors(machines []string) {
	newMachines := map[string]struct{}{}
	for _, nm := range machines {
		newMachines[nm] = struct{}{}
	}

	for m, c := range collectors {
		if _, ok := newMachines[m]; !ok {
			prometheus.Unregister(c)
			delete(collectors, m)
		} else {
			delete(newMachines, m)
		}
	}

	for nm := range newMachines {
		e := NewExporter(nm)
		prometheus.MustRegister(e)
		collectors[nm] = e
	}
}
