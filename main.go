package main

import (
	// "encoding/json"
	"flag"
	"log"
	"net/http"

	"github.com/coreos/go-etcd/etcd"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	etcdAddress   = flag.String("etcd.address", "http://127.0.0.1:7001", "Address of the initial etcd instance.")
	listenAddress = flag.String("web.listen-address", ":9105", "Address to listen on for web interface and telemetry.")
	metricsPath   = flag.String("web.telemetry-path", "/metrics", "Path under which to expose metrics.")
	// discoveryURL  = flag.String("discovery", "", "The etcd discovery service URL for the cluster.")

	collectors = map[string]prometheus.Collector{}
)

func main() {
	flag.Parse()

	// machines, err := discover(*discoveryURL)
	// if err != nil {
	// 	log.Fatalf("error discovering cluster at %s: %s", *discoveryURL, err)
	// }
	client := etcd.NewClient([]string{*etcdAddress})
	client.SyncCluster()
	refreshCollectors(client.GetCluster())

	recv := make(chan *etcd.Response)
	_, err := client.Watch("/_etcd/machines/", 0, true, recv, nil)
	if err != nil {
		log.Fatalf("error watching machines: %s", err)
	}
	// keep refreshing the participating servers
	go func() {
		for {
			<-recv
			if client.SyncCluster() {
				refreshCollectors(client.GetCluster())
			}
		}
	}()

	http.Handle(*metricsPath, prometheus.Handler())
	err = http.ListenAndServe(*listenAddress, nil)
	if err != nil {
		log.Fatal(err)
	}
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
	}
}

// // discover gets the internal addresses of the machines currently in the cluster
// func discover(durl string) ([]string, error) {
// 	res, err := http.Get(*discoveryURL)
// 	if err != nil {
// 		return nil, err
// 	}
// 	dec := json.NewDecoder(res.Body)
// 	defer res.Body.Close()

// 	var r struct {
// 		Node struct {
// 			Nodes []struct {
// 				Value string `json:"value"`
// 			} `json:"nodes"`
// 		} `json:"node"`
// 	}
// 	err = dec.Decode(&r)
// 	if err != nil {
// 		return nil, err
// 	}

// 	machines := make([]string, 0)
// 	for _, n := range r.Node.Nodes {
// 		machines = append(machines, n.Value)
// 	}
// 	return machines, nil
// }
