package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestExporterError(t *testing.T) {
	var delay time.Duration

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-time.After(delay)
		fmt.Fprintf(w, "malformed JSON content")
	}))
	defer ts.Close()

	delay = 10 * time.Millisecond
	e := NewExporter(ts.URL, 5*time.Millisecond)
	e.scraping = true
	e.scrapeAll()
	if e.scraping {
		t.Fatalf("scraping status not reset on timeout")
	}

	delay = 0
	e = NewExporter(ts.URL, 0)
	e.scraping = true
	e.scrapeAll()
	if e.scraping {
		t.Fatalf("scraping not reset on decoding error")
	}
}

func TestExporterFastCollect(t *testing.T) {
	var (
		block = make(chan bool)
		reqs  = make(chan bool, 10)
	)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs <- true
		<-block
	}))
	defer ts.Close()

	// check if calling collect multiple before a scrape can finish accumulates scrapes
	e := NewExporter(ts.URL, 0)
	cch := make(chan prometheus.Metric, 100)

	e.Collect(cch)
	e.Collect(cch)
	e.Collect(cch)
	<-reqs
	close(block)

	if len(reqs) != 0 {
		t.Fatalf("expected %d executed scrape(s), got %d", 1, len(reqs)+1)
	}
}

func TestCollector(t *testing.T) {
	c := newCollector("test")

	c.gaugeVec("test_gauge", "test_help", nil)
	if v, ok := c.gaugeVecs["test_gauge"]; !ok {
		t.Fatalf("registered gauge vector missing")
	} else {
		v.With(nil).Set(1)
	}

	c.summaryVec("test_summary", "test_help", nil)
	if v, ok := c.summaryVecs["test_summary"]; !ok {
		t.Fatalf("registered summary vector missing")
	} else {
		v.With(nil).Observe(1)
	}

	c.counterVec("test_counter", "test_help", nil)
	if v, ok := c.counterVecs["test_counter"]; !ok {
		t.Fatalf("registered counter vector missing")
	} else {
		v.With(nil).Inc()
	}

	dch := make(chan *prometheus.Desc, 10)
	c.Describe(dch)
	if len(dch) != 3 {
		t.Fatalf("inserted %d metrics but %d were described", 3, len(dch))
	}

	cch := make(chan prometheus.Metric, 10)
	c.Collect(cch)
	if len(cch) != 3 {
		t.Fatalf("inserted %d metrics but %d were collected", 3, len(cch))
	}
}

func TestTime3339(t *testing.T) {
	ts := "2014-01-01T15:26:24.96569404Z"

	var tmp Time3339
	err := json.Unmarshal([]byte("\""+ts+"\""), &tmp)
	if err != nil {
		t.Fatalf("error decoding JSON timestamp: %s", err)
	}

	tmc, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t.Fatalf("error parsing timestamp: %s", err)
	}
	if !tmc.Equal(time.Time(tmp)) {
		t.Fatalf("decoded time mismatch: got %s, expected %s", tmp, tmc)
	}

	err = json.Unmarshal([]byte("null"), &tmp)
	if err != nil {
		t.Fatalf("error decoding JSON timestamp: %s", err)
	}
	if !time.Time(tmp).Equal(time.Time{}) {
		t.Fatalf("null not parsed to zero time, got %s", tmp)
	}

	err = json.Unmarshal([]byte("\"\""), &tmp)
	if err != nil {
		t.Fatalf("error decoding JSON timestamp: %s", err)
	}
	if !time.Time(tmp).Equal(time.Time{}) {
		t.Fatalf("empty string not parsed to zero time, got %s", tmp)
	}
}
