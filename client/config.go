package main

import (
	"time"
)

var (
	validURISchemes = []string{
		"http", "https",
	}
	closeGracePeriod = time.Second * 2
)
