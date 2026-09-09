//go:build windows && lunitide_e2e

package main

func gatewayInstanceMutexName() string {
	return "Local\\lunitide-gateway-e2e"
}
