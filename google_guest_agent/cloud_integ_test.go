// +build integration

package main

import "testing"

func TestCloudSkewManager(t *testing.T) {
	var c = clockskewMgr{}
	if err := c.set(); err != nil {
		t.Errorf("failed to run cloud skew manager")
	}
}
