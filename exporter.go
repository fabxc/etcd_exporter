package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	namespace = "etcd"

	stateFollower = "follower"
	stateLeader   = "leader"

	pathSelfStats   = "/v2/stats/self"
	pathLeaderStats = "/v2/stats/leader"
	pathStoreStats  = "/v2/stats/store"
)

type exporter struct {
	client *http.Client
	addr   string

	up           prometheus.Gauge
	totalScrapes prometheus.Counter

	selfMetrics *selfMetrics
}

func NewExporter(addr string) *exporter {
	constLabels := prometheus.Labels{
		"instance": addr,
	}

	e := &exporter{
		client: &http.Client{},
		addr:   addr,

		up: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace:   namespace,
			Name:        "up",
			Help:        "Was the last scrape of etcd successful.",
			ConstLabels: constLabels,
		}),
		totalScrapes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace:   namespace,
			Name:        "exporter_total_scrapes",
			Help:        "Current total etcd scrapes.",
			ConstLabels: constLabels,
		}),

		selfMetrics: newSelfMetrics(constLabels),
		// storeMetrics: newStoreMetrics(),
	}

	return e
}

// scrape retrieves the JSON stats from the given endpoint
// and decodes them into v.
func (e *exporter) scrape(path string, v interface{}) error {
	resp, err := e.client.Get("http://" + e.addr + path)
	if err != nil {
		return err
	}

	dec := json.NewDecoder(resp.Body)
	defer resp.Body.Close()

	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}

func (e *exporter) Describe(ch chan<- *prometheus.Desc) {
	e.selfMetrics.Describe(ch)
	// e.leaderMetrics.Describe(ch)
	// e.storeMetrics.Describe(ch)

	ch <- e.up.Desc()
	ch <- e.totalScrapes.Desc()
}

func (e *exporter) Collect(ch chan<- prometheus.Metric) {
	defer func() {
		ch <- e.up
		ch <- e.totalScrapes
	}()

	var selfs selfStats
	err := e.scrape(pathSelfStats, &selfs)
	if err != nil {
		log.Println("error scraping etcd process:", err)
		e.up.Set(0)
		return
	}
	e.selfMetrics.set(&selfs)
	e.selfMetrics.Collect(ch)

	// var stores storeStats
	// err = e.scrape(pathStoreStats, &stores)
	// if err != nil {
	// 	log.Println("error scraping etcd process:", err)
	// 	e.up.Set(0)
	// 	return
	// }
	// e.storeMetrics.set(&stores)
	// e.storeMetrics.Collect(ch)

	// if ss.State == stateLeader {

	// }

	e.up.Set(1)
	e.totalScrapes.Inc()
}

type selfStats struct {
	Name                   string  `json:"name"`
	State                  string  `json:"state"`
	RecvAppendRequestCount int64   `json:"recvAppendRequestCnt"`
	SendAppendRequestCount int64   `json:"sendAppendRequestCnt"`
	RecvBandwidthRate      float64 `json:"recvBandwidthRate"`
	RecvPkgRate            float64 `json:"recvPkgRate"`
	// StartTime              timeRFC3339 `json:"startTime"`
}

type selfMetrics struct {
	RecvAppendRequestCount *prometheus.GaugeVec
	SendAppendRequestCount *prometheus.GaugeVec
	RecvBandwidthRate      *prometheus.SummaryVec
	RecvPkgRate            *prometheus.SummaryVec
	// Uptime                 *prometheus.GaugeVec
}

func newSelfMetrics(constLabels prometheus.Labels) *selfMetrics {
	return &selfMetrics{
		RecvAppendRequestCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace:   namespace,
			Subsystem:   "self",
			Name:        "recv_append_request_count",
			Help:        "help",
			ConstLabels: constLabels,
		}, []string{"name", "state"}),

		SendAppendRequestCount: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace:   namespace,
			Subsystem:   "self",
			Name:        "send_append_request_count",
			Help:        "help",
			ConstLabels: constLabels,
		}, []string{"name", "state"}),

		RecvBandwidthRate: prometheus.NewSummaryVec(prometheus.SummaryOpts{
			Namespace:   namespace,
			Subsystem:   "self",
			Name:        "recv_bandwidth_rate",
			Help:        "help",
			ConstLabels: constLabels,
		}, []string{"name", "state"}),

		RecvPkgRate: prometheus.NewSummaryVec(prometheus.SummaryOpts{
			Namespace:   namespace,
			Subsystem:   "self",
			Name:        "recv_pkg_rate",
			Help:        "help",
			ConstLabels: constLabels,
		}, []string{"name", "state"}),
	}
}

func (sm *selfMetrics) set(ss *selfStats) {
	sm.RecvAppendRequestCount.WithLabelValues(ss.Name, ss.State).Set(float64(ss.RecvAppendRequestCount))
	sm.SendAppendRequestCount.WithLabelValues(ss.Name, ss.State).Set(float64(ss.SendAppendRequestCount))

	sm.RecvBandwidthRate.WithLabelValues(ss.Name, ss.State).Observe(ss.RecvBandwidthRate)
	sm.RecvPkgRate.WithLabelValues(ss.Name, ss.State).Observe(ss.RecvPkgRate)

	// sm.Uptime.WithLabelValues(ss.Name, ss.State).Set(timeRFC3339.Diff(time.Now()))
}

func (sm *selfMetrics) Describe(ch chan<- *prometheus.Desc) {
	sm.RecvAppendRequestCount.Describe(ch)
	sm.SendAppendRequestCount.Describe(ch)
	sm.RecvBandwidthRate.Describe(ch)
	sm.RecvPkgRate.Describe(ch)
}

func (sm *selfMetrics) Collect(ch chan<- prometheus.Metric) {
	sm.RecvAppendRequestCount.Collect(ch)
	sm.SendAppendRequestCount.Collect(ch)
	sm.RecvBandwidthRate.Collect(ch)
	sm.RecvPkgRate.Collect(ch)
}

type storeStats map[string]int64

type storeMetrics map[string]prometheus.Gauge

// func newStoreMetrics() storeMetrics {
// 	newGauge := func(name, help string) prometheus.Gauge {
// 		return prometheus.NewGauge(prometheus.GaugeOpts{
// 			Namespace: namespace,
// 			Subsystem: "store",
// 			Name:      name,
// 			Help: help,
// 		})
// 	}

// 	for _, name := range []string{
// 		"compare_and_swap_fail"
// 	} {

// 		}

// 	return []prometheus.Gauge{
// 		"compare_and_swap_fail": prometheus.NewGauge(prometheus.GaugeOpts{
// 			Namespace: namespace
// 		})
// 	}
// }

var storeMapping = map[string]string{
	"compareAndSwapFail":    "compare_and_swap_fail",
	"compareAndSwapSuccess": "compare_and_swap_success",
	"createFail":            "create_fail",
	"createSuccess":         "create_success",
	"deleteFail":            "delete_fail",
	"deleteSuccess":         "delete_success",
	"expireCount":           "expire_count",
	"getsFail":              "gets_fail",
	"getsSuccess":           "gets_success",
	"setsFail":              "sets_fail",
	"setsSuccess":           "sets_success",
	"updateFail":            "update_fail",
	"updateSuccess":         "update_success",
	"watchers":              "watchers",
}

// func (sm storeMetrics) set(ss storeStats) {
// 	for k, v := range ss {
// 		if nk, ok := storeMapping[k]; ok {
// 			sm[nk].Set(v)
// 		}
// 	}
// }
