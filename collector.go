package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	namespace = "etcd"

	stateFollower = "follower"
	stateLeader   = "leader"

	endpointSelfStats   = "/v2/stats/self"
	endpointLeaderStats = "/v2/stats/leader"
	endpointStoreStats  = "/v2/stats/store"
)

type exporter struct {
	client   *http.Client
	addr     string
	mutex    sync.RWMutex
	scraping bool

	up             prometheus.Gauge
	totalScrapes   prometheus.Counter
	scrapeDuration prometheus.Summary

	selfMetrics   *selfMetrics
	storeMetrics  *storeMetrics
	leaderMetrics *leaderMetrics
}

func NewExporter(addr string, timeout time.Duration) *exporter {
	return &exporter{
		client: &http.Client{
			Timeout: timeout,
		},
		addr: addr,

		up: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace:   namespace,
			Name:        "up",
			Help:        "Was the last scrape of etcd successful.",
			ConstLabels: prometheus.Labels{"host": addr},
		}),
		totalScrapes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace:   namespace,
			Name:        "exporter_total_scrapes",
			Help:        "Total number of scrapes for the node.",
			ConstLabels: prometheus.Labels{"host": addr},
		}),
		scrapeDuration: prometheus.NewSummary(prometheus.SummaryOpts{
			Namespace:   namespace,
			Name:        "exporter_scrape_duration_seconds",
			Help:        "Duration of last scrape.",
			ConstLabels: prometheus.Labels{"host": addr},
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

// Describe implements the prometheus.Collector interface.
func (e *exporter) Describe(ch chan<- *prometheus.Desc) {
	e.selfMetrics.Describe(ch)
	e.leaderMetrics.Describe(ch)
	e.storeMetrics.Describe(ch)

	ch <- e.up.Desc()
	ch <- e.totalScrapes.Desc()
	ch <- e.scrapeDuration.Desc()
}

// scrapeAll scrapes all endpoints and provides new stats through the scrapedStats chan.
func (e *exporter) scrapeAll() {
	var err error
	start := time.Now()
	// mark instance as down on scrape error
	defer func() {
		e.mutex.Lock()

		e.totalScrapes.Inc()
		e.scrapeDuration.Observe(time.Since(start).Seconds())

		if err != nil {
			log.Printf("exporter %s: error scraping etcd process: %s", e.addr, err)
			e.up.Set(0)
		} else {
			e.up.Set(1)
		}
		e.scraping = false
		e.mutex.Unlock()
	}()

	e.selfMetrics.Reset()
	e.leaderMetrics.Reset()
	e.storeMetrics.Reset()

	ses := new(selfStats)
	err = e.scrape(endpointSelfStats, ses)
	if err != nil {
		return
	}
	sts := make(map[string]int64)
	err = e.scrape(endpointStoreStats, &sts)
	if err != nil {
		return
	}
	ls := new(leaderSats)
	if ses.State == stateLeader {
		err = e.scrape(endpointLeaderStats, ls)
		if err != nil {
			return
		}
	}

	e.mutex.Lock()
	defer e.mutex.Unlock()

	e.selfMetrics.set(ses)
	e.storeMetrics.set(sts)
	if ses.State == stateLeader {
		e.leaderMetrics.set(ls)
	}
}

// Collect implements the prometheus.Collector interface.
func (e *exporter) Collect(ch chan<- prometheus.Metric) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	// do not accumulate a scrape queue
	if !e.scraping {
		go e.scrapeAll()
		e.scraping = true
	}

	e.selfMetrics.Collect(ch)
	e.leaderMetrics.Collect(ch)
	e.storeMetrics.Collect(ch)

	ch <- e.up
	ch <- e.totalScrapes
	ch <- e.scrapeDuration
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
			// Average float64 `json:"average"`
			Current float64 `json:"current"`
			Maximum float64 `json:"maximum"`
			Minimum float64 `json:"minimum"`
			// Stddev  float64 `json:"standardDeviation"`
		} `json:"latency"`
	} `json:"followers"`
}

// leaderMetrics contains metrics based on etcd's leader stats. As they are only
// scraped from the leader node they have a different set of constant labels.
type leaderMetrics struct {
	collector
}

func newLeaderMetrics(addr string) *leaderMetrics {
	c := newCollector("leader")

	constLabels := prometheus.Labels{"host": addr}
	labels := []string{"leader_id", "follower_id"}

	c.counterVec("follower_fail_total", "Total number of failed Raft RPC requests.",
		constLabels, labels...)
	c.counterVec("follower_success_total", "Total number of successful Raft RPC requests.",
		constLabels, labels...)

	c.gaugeVec("follower_latency_last_milliseconds", "Last measured latency of the follower to the leader.",
		constLabels, labels...)
	// c.gaugeVec("follower_latency_milliseconds_avg", "Current latency average of the follower to the leader.",
	// 	constLabels, labels...)
	c.gaugeVec("follower_latency_milliseconds_min", "Current latency maximum of the follower to the leader.",
		constLabels, labels...)
	c.gaugeVec("follower_latency_milliseconds_max", "Current latency minimum of the follower to the leader.",
		constLabels, labels...)
	// c.gaugeVec("follower_latency_milliseconds_stddev", "Current latency standard deviation of the follower to the leader.",
	// 	constLabels, labels...)

	return &leaderMetrics{c}
}

func (m *leaderMetrics) set(stats *leaderSats) {
	lid := stats.Leader
	set := func(dst, fid string, v float64) {
		m.gaugeVecs[dst].WithLabelValues(lid, fid).Set(v)
	}

	for fid, fs := range stats.Followers {
		m.counterVecs["follower_fail_total"].WithLabelValues(lid, fid).Set(float64(fs.Counts.Fail))
		m.counterVecs["follower_success_total"].WithLabelValues(lid, fid).Set(float64(fs.Counts.Success))

		set("follower_latency_last_milliseconds", fid, fs.Latency.Current)
		// set("follower_latency_milliseconds_avg", fid, fs.Latency.Average)
		set("follower_latency_milliseconds_min", fid, fs.Latency.Minimum)
		set("follower_latency_milliseconds_max", fid, fs.Latency.Maximum)
		// set("follower_latency_milliseconds_stddev", fid, fs.Latency.Stddev)
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
	c := newCollector("self")

	constLabels := prometheus.Labels{"host": addr}

	c.gaugeVec("node", "Further properties of the etcd node.", constLabels, "name", "id")
	c.gaugeVec("leader", "Whether the node is the leader or a follower.", constLabels)

	c.counterVec("recv_append_requests_total", "Total number of received append requests.", constLabels)
	c.counterVec("send_append_requests_total", "Total number of sent append requests.", constLabels)
	c.counterVec("uptime_seconds", "Uptime of the node in seconds.", constLabels)

	c.gaugeVec("recv_bandwidth_bytes_rate", "Bytes/second this node is receiving in total.", constLabels)
	c.gaugeVec("recv_pkg_rate", "Requests/second this not is receiving in total", constLabels)

	c.gaugeVec("send_bandwidth_bytes_rate", "Bytes/second this node is sending in total.", constLabels)
	c.gaugeVec("send_pkg_rate", "Requests/second this node is sending in total.", constLabels)

	return &selfMetrics{c}
}

func (m *selfMetrics) set(ss *selfStats) {
	m.gaugeVecs["node"].WithLabelValues(ss.Name, ss.ID).Set(1)

	m.counterVecs["recv_append_requests_total"].With(nil).Set(float64(ss.RecvAppendRequestCount))
	m.counterVecs["send_append_requests_total"].With(nil).Set(float64(ss.SendAppendRequestCount))

	tdiff := time.Since(time.Time(ss.StartTime))
	m.counterVecs["uptime_seconds"].With(nil).Set(tdiff.Seconds())

	if ss.State == stateFollower {
		m.gaugeVecs["leader"].With(nil).Set(0)
		m.gaugeVecs["recv_bandwidth_bytes_rate"].With(nil).Set(ss.RecvBandwidthRate)
		m.gaugeVecs["recv_pkg_rate"].With(nil).Set(ss.RecvPkgRate)
	}
	if ss.State == stateLeader {
		m.gaugeVecs["leader"].With(nil).Set(1)
		m.gaugeVecs["send_bandwidth_bytes_rate"].With(nil).Set(ss.SendBandwidthRate)
		m.gaugeVecs["send_pkg_rate"].With(nil).Set(ss.SendPkgRate)
	}
}

// storeMetrics holds metrics based on etcd's store stats.
type storeMetrics struct {
	collector
}

func newStoreMetrics(addr string) *storeMetrics {
	c := newCollector("store")

	constLabels := prometheus.Labels{"host": addr}

	c.counterVec("expire_total", "Total number of expiries.", constLabels)
	c.gaugeVec("watchers", "Current number of watchers.", constLabels)

	// TODO: the global counters should be equal for all nodes. If they can differ between nodes
	// this might be an interesting metric. Otherwise they should only be scraped once without
	// any labels. This might require some register/unregister to avoid duplicate metric errors
	// while still being able to switch the node they are scraped from.
	c.counterVec("fail_total", "Total number of failed operations",
		constLabels, "operation")
	c.counterVec("success_total", "Total number of successful operations",
		constLabels, "operation")

	return &storeMetrics{c}
}

func (m *storeMetrics) set(stats map[string]int64) {
	setOp := func(op string, src string) {
		m.counterVecs["fail_total"].WithLabelValues(op).Set(float64(stats[src+"Fail"]))
		m.counterVecs["success_total"].WithLabelValues(op).Set(float64(stats[src+"Success"]))
	}
	// global
	m.counterVecs["expire_total"].With(nil).Set(float64(stats["expireCount"]))

	setOp("compare_and_swap", "compareAndSwap")
	setOp("create", "create")
	setOp("set", "sets")
	setOp("update", "update")
	setOp("delete", "delete")

	// per-node
	m.gaugeVecs["watchers"].With(nil).Set(float64(stats["watchers"]))

	setOp("get", "gets")
}

// collector holds GaugeVecs and SummaryVecs and provides functionality
// to reset, collect, and describe them at once.
type collector struct {
	subsystem string

	counterVecs map[string]*prometheus.CounterVec
	gaugeVecs   map[string]*prometheus.GaugeVec
	summaryVecs map[string]*prometheus.SummaryVec
}

func newCollector(subsys string) collector {
	return collector{
		subsystem:   subsys,
		counterVecs: make(map[string]*prometheus.CounterVec),
		gaugeVecs:   make(map[string]*prometheus.GaugeVec),
		summaryVecs: make(map[string]*prometheus.SummaryVec),
	}
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

// counterVec registers a new CounterVec for the collector.
func (c *collector) counterVec(name, help string, constLabels prometheus.Labels, labels ...string) {
	c.counterVecs[name] = prometheus.NewCounterVec(prometheus.CounterOpts{
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
	for _, v := range c.counterVecs {
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
	for _, v := range c.counterVecs {
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
