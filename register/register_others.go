//go:build !windows

package register

import "fmt"

var serviceStop = make(chan struct{})

func Configure(registerService, unregisterService bool) (bool, error) {
	if registerService || unregisterService {
		return false, fmt.Errorf("Windows service registration is unavailable on this platform")
	}
	return false, nil
}

func StopChannel() <-chan struct{} {
	return serviceStop
}
