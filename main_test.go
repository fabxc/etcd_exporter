package main

import (
	"testing"
)

func TestCollectorsMap(t *testing.T) {
	ec := etcdCollectors{}

	// test deduplication
	ec.refresh([]string{"http://my.machine:4001", "http://my.machine:2380"})
	if len(ec) != 1 {
		t.Errorf("want same machine added exactly once")
	}

	// test join
	ec.refresh([]string{"http://my.machine:2380", "http://my.machine2:1234"})
	if len(ec) != 2 {
		t.Errorf("join of new machine failed")
	}

	// test removal
	ec.refresh([]string{})
	if len(ec) != 0 {
		t.Errorf("machines were not cleared from map")
	}
}
