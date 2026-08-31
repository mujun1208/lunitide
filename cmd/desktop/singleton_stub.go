//go:build !windows

package main

import "time"

func claimGatewayInstance() (already bool, release func()) {
	return false, func() {}
}

func claimGatewayInstanceRetry(time.Duration) (already bool, release func()) {
	return claimGatewayInstance()
}
