//go:build !windows

package main

func claimGatewayInstance() (already bool, release func()) {
	return false, func() {}
}
