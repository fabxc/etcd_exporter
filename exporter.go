package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	namespace = "etcd"

	stateFollower = "StateFollower"
	stateLeader   = "StateLeader"

	endpointSelfStats   = "/v2/stats/self"
	endpointLeaderStats = "/v2/stats/leader"
	endpointStoreStats  = "/v2/stats/store"
)

type exporter struct {
	client *http.Client
	addr   string

	up           prometheus.Gauge
	totalScrapes prometheus.Counter

	selfMetrics   *selfMetrics
	storeMetrics  *storeMetrics
	leaderMetrics *leaderMetrics
}

func NewExporter(addr string) *exporter {
	return &exporter{
		client: &http.Client{},
		addr:   addr,

		up: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace:   namespace,
			Name:        "up",
			Help:        "Was the last scrape of etcd successful.",
			ConstLabels: prometheus.Labels{"instance": addr},
		}),
		totalScrapes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace:   namespace,
			Name:        "exporter_total_scrapes",
			Help:        "Total number of scrapes for the node.",
			ConstLabels: prometheus.Labels{"instance": addr},
		}),

		selfMetrics:   newSelfMetrics(addr),
		storeMetrics:  newStoreMetrics(addr),
		leaderMetrics: newLeaderMetrics(addr),
	}
}

// scrape retrieves the JSON stats from the given endpoint
// and decodes them into v.
func (e *exporter) scrape(endpoint string, v interface{}) error {
	resp, err := e.client.Get(e.addr + endpoint)
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
	e.leaderMetrics.Describe(ch)
	e.storeMetrics.Describe(ch)

	ch <- e.up.Desc()
	ch <- e.totalScrapes.Desc()
}

func (e *exporter) Collect(ch chan<- prometheus.Metric) {
	var err error
	e.totalScrapes.Inc()

	defer func() {
		if err != nil {
			log.Printf("exporter %s: error scraping etcd process:", e.addr, err)
			e.up.Set(0)
		} else {
			e.up.Set(1)
		}
		ch <- e.up
		ch <- e.totalScrapes
	}()

	e.selfMetrics.Reset()
	e.storeMetrics.Reset()
	e.leaderMetrics.Reset()

	ses := new(selfStats)
	err = e.scrape(endpointSelfStats, ses)
	if err != nil {
		return
	}
	e.selfMetrics.set(ses, ses.Name, ses.ID, ses.State)
	e.selfMetrics.Collect(ch)

	sts := make(map[string]int64)
	err = e.scrape(endpointStoreStats, &sts)
	if err != nil {
		return
	}
	e.storeMetrics.set(sts, ses.Name, ses.ID, ses.State)
	e.storeMetrics.Collect(ch)

	if ses.State == stateLeader {
		ls := new(leaderSats)
		err = e.scrape(endpointLeaderStats, ls)
		if err != nil {
			return
		}
		e.leaderMetrics.set(ls, ls.Leader)
		e.leaderMetrics.Collect(ch)
	}
}

// leaderStats holds etcd's leader stats information.
type leaderSats struct {
	Leader string `json:"leader"`

	Followers map[string]struct {
		Counts struct {
			Fail    int64 `json:"fail"`
			Success int64 `json:"success"`
		} `json:"counts"`

		Latency struct {
			Current float64 `json:"current"`
		} `json:"latency"`
	} `json:"followers"`
}

// leaderMetrics contains metrics based on etcd's leader stats. As they are only
// scraped from the leader node they have a different set of constant labels.
type leaderMetrics struct {
	collector
}

func newLeaderMetrics(addr string) *leaderMetrics {
	c := collector{
		subsystem:   "leader",
		gaugeVecs:   make(map[string]*prometheus.GaugeVec),
		summaryVecs: make(map[string]*prometheus.SummaryVec),
	}
	constLabels := prometheus.Labels{"instance": addr}
	labels := []string{"leader_id", "follower_id"}

	c.gaugeVec("follower_fail_total", "Total number of failed Raft RPC requests.",
		constLabels, labels...)
	c.gaugeVec("follower_success_total", "Total number of successful Raft RPC requests.",
		constLabels, labels...)

	c.summaryVec("follower_latency_milliseconds", "Current latency of the follower to the leader.",
		constLabels, labels...)

	return &leaderMetrics{c}
}

func (m *leaderMetrics) set(stats *leaderSats, lid string) {
	for fid, fs := range stats.Followers {
		m.gaugeVecs["follower_fail_total"].WithLabelValues(lid, fid).Set(float64(fs.Counts.Fail))
		m.gaugeVecs["follower_success_total"].WithLabelValues(lid, fid).Set(float64(fs.Counts.Success))

		m.summaryVecs["follower_latency_milliseconds"].WithLabelValues(lid, fid).Observe(float64(fs.Latency.Current))
	}
}

// selfStats holds etcd's leader stats information
type selfStats struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`

	// common fields
	RecvAppendRequestCount int64    `json:"recvAppendRequestCnt"`
	SendAppendRequestCount int64    `json:"sendAppendRequestCnt"`
	StartTime              Time3339 `json:"startTime"`

	// leader-only fields
	SendBandwidthRate float64 `json:"sendBandwidthRate"`
	SendPkgRate       float64 `json:"sendPkgRate"`

	// follower-only fields
	RecvBandwidthRate float64 `json:"recvBandwidthRate"`
	RecvPkgRate       float64 `json:"recvPkgRate"`
}

// selfMetrics holds metrics based on etcd's self stats.
type selfMetrics struct {
	collector
}

func newSelfMetrics(addr string) *selfMetrics {
	c := collector{
		subsystem:   "self",
		gaugeVecs:   make(map[string]*prometheus.GaugeVec),
		summaryVecs: make(map[string]*prometheus.SummaryVec),
	}
	constLabels := prometheus.Labels{"instance": addr}
	labels := []string{"name", "id", "state"}

	c.gaugeVec("recv_append_requests_total", "Total number of received append requests.",
		constLabels, labels...)
	c.gaugeVec("send_append_requests_total", "Total number of sent append requests.",
		constLabels, labels...)
	c.gaugeVec("uptime_seconds", "Uptime of the node in seconds.",
		constLabels, labels...)

	c.summaryVec("recv_bandwidth_bytes_rate", "Receiving rate in bytes/second.",
		constLabels, "name", "id")
	c.summaryVec("recv_pkg_rate", "Receiving rate in requests/second.",
		constLabels, "name", "id")

	c.summaryVec("send_bandwidth_bytes_rate", "Sending rate in bytes/second.",
		constLabels, "name", "id")
	c.summaryVec("send_pkg_rate", "Sending rate in requests/second.",
		constLabels, "name", "id")

	return &selfMetrics{c}
}

func (m *selfMetrics) set(ss *selfStats, name, id, state string) {
	m.gaugeVecs["recv_append_requests_total"].WithLabelValues(name, id, state).Set(float64(ss.RecvAppendRequestCount))
	m.gaugeVecs["send_append_requests_total"].WithLabelValues(name, id, state).Set(float64(ss.SendAppendRequestCount))

	tdiff := time.Since(time.Time(ss.StartTime))
	m.gaugeVecs["uptime_seconds"].WithLabelValues(name, id, state).Set(tdiff.Seconds())

	if state == stateFollower {
		m.summaryVecs["recv_bandwidth_bytes_rate"].WithLabelValues(name, id).Observe(ss.RecvBandwidthRate)
		m.summaryVecs["recv_pkg_rate"].WithLabelValues(name, id).Observe(ss.RecvPkgRate)
	}
	if state == stateLeader {
		m.summaryVecs["send_bandwidth_bytes_rate"].WithLabelValues(name, id).Observe(ss.SendBandwidthRate)
		m.summaryVecs["send_pkg_rate"].WithLabelValues(name, id).Observe(ss.SendPkgRate)
	}
}

// storeMetrics holds metrics based on etcd's store stats.
type storeMetrics struct {
	collector
}

func newStoreMetrics(addr string) *storeMetrics {
	c := collector{
		subsystem:   "store",
		gaugeVecs:   make(map[string]*prometheus.GaugeVec),
		summaryVecs: make(map[string]*prometheus.SummaryVec),
	}
	constLabels := prometheus.Labels{"instance": addr}
	labels := []string{"name", "id", "state"}

	// global counters
	//
	// TODO: the global counters should be equal for all nodes. If they can differ between nodes
	// this might be an interesting metric. Otherwise they should only be scraped once without
	// any labels. This might require some register/unregister to avoid duplicate metric errors
	// while still being able to switch the node they are scraped from.
	c.gaugeVec("compare_and_swap_fail_total", "Total number of failed compare-and-swap operations.",
		constLabels, labels...)
	c.gaugeVec("compare_and_swap_success_total", "Total number of successful compare-and-swap operations.",
		constLabels, labels...)

	c.gaugeVec("create_fail_total", "Total number of failed create operations.",
		constLabels, labels...)
	c.gaugeVec("create_success_total", "Total number of successful create operations.",
		constLabels, labels...)

	c.gaugeVec("delete_fail_total", "Total number of failed delete operations.",
		constLabels, labels...)
	c.gaugeVec("delete_success_total", "Total number of successful delete operations.",
		constLabels, labels...)

	c.gaugeVec("expire_count_total", "Total number of expiries.",
		constLabels, labels...)

	c.gaugeVec("sets_fail_total", "Total number of failed set operations.",
		constLabels, labels...)
	c.gaugeVec("sets_success_total", "Total number of successful set operations.",
		constLabels, labels...)

	c.gaugeVec("update_fail_total", "Total number of failed update operations.",
		constLabels, labels...)
	c.gaugeVec("update_success_total", "Total number of successful update operations.",
		constLabels, labels...)

	// per-node counters
	c.gaugeVec("gets_fail_total", "Total number of failed get operations.",
		constLabels, labels...)
	c.gaugeVec("gets_success_total", "Total number of successful getoperations.",
		constLabels, labels...)

	c.gaugeVec("watchers", "Current number of watchers.",
		constLabels, labels...)

	return &storeMetrics{c}
}

func (m *storeMetrics) set(stats map[string]int64, name, id, state string) {
	set := func(dst, src string) {
		m.gaugeVecs[dst].WithLabelValues(name, id, state).Set(float64(stats[src]))
	}
	set("compare_and_swap_fail_total", "compareAndSwapFail")
	set("compare_and_swap_success_total", "compareAndSwapSuccess")
	set("create_fail_total", "createFail")
	set("create_success_total", "createSuccess")
	set("delete_fail_total", "deleteFail")
	set("delete_success_total", "deleteSuccess")
	set("expire_count_total", "expireCount")
	set("sets_fail_total", "setsFail")
	set("sets_success_total", "setsSuccess")
	set("update_fail_total", "updateFail")
	set("update_success_total", "updateSuccess")

	set("gets_fail_total", "getsFail")
	set("gets_success_total", "getsSuccess")
	set("watchers", "watchers")
}

// collector holds GaugeVecs and SummaryVecs and provides functionality
// to reset, collect, and describe them at once.
type collector struct {
	subsystem string

	gaugeVecs   map[string]*prometheus.GaugeVec
	summaryVecs map[string]*prometheus.SummaryVec
}

// gaugeVec registers a new GaugeVec for the collector.
func (c *collector) gaugeVec(name, help string, constLabels prometheus.Labels, labels ...string) {
	c.gaugeVecs[name] = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace:   namespace,
		Subsystem:   c.subsystem,
		Name:        name,
		Help:        help,
		ConstLabels: constLabels,
	}, labels)
}

// summaryVec registers a new SummaryVec for the collector.
func (c *collector) summaryVec(name, help string, constLabels prometheus.Labels, labels ...string) {
	c.summaryVecs[name] = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Namespace:   namespace,
		Subsystem:   c.subsystem,
		Name:        name,
		Help:        help,
		ConstLabels: constLabels,
	}, labels)
}

// Describe implements the prometheus.Collector interface
func (c *collector) Describe(ch chan<- *prometheus.Desc) {
	for _, v := range c.gaugeVecs {
		v.Describe(ch)
	}
	for _, v := range c.summaryVecs {
		v.Describe(ch)
	}
}

// Collect implements the prometheus.Collector interface..
func (c *collector) Collect(ch chan<- prometheus.Metric) {
	for _, v := range c.gaugeVecs {
		v.Collect(ch)
	}
	for _, v := range c.summaryVecs {
		v.Collect(ch)
	}
}

func (c *collector) Reset() {
	for _, v := range c.gaugeVecs {
		v.Reset()
	}
}

// Time3339 is a time.Time which encodes an RFC3999 formatted timestamp to UTC time.
type Time3339 time.Time

var _ json.Unmarshaler = (*Time3339)(nil)

func (t Time3339) String() string {
	return time.Time(t).UTC().Format(time.RFC3339Nano)
}

func (t *Time3339) UnmarshalJSON(b []byte) error {
	s := string(b)
	if s == "null" {
		*t = Time3339{}
		return nil
	}
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return fmt.Errorf("no valid string for timestamp decoding.")
	}
	s = s[1 : len(s)-1]
	if len(s) == 0 {
		*t = Time3339{}
		return nil
	}
	tm, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return err
	}
	*t = Time3339(tm)
	return nil
}
