//go:build windows

package register

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	serviceName        = "pasturestack-per-host-subnet"
	serviceDisplayName = "PastureStack Per-Host Subnet"
	serviceDescription = "Maintains host subnet routes and platform network mappings."
)

var (
	serviceStop     = make(chan struct{})
	serviceStopOnce sync.Once
)

// Configure applies a requested registration action or starts the Windows
// service dispatcher when this process was launched by the service manager.
// handled is true when a registration action completed and normal startup must
// stop.
func Configure(registerServiceFlag, unregisterServiceFlag bool) (handled bool, err error) {
	if registerServiceFlag && unregisterServiceFlag {
		return false, fmt.Errorf("register-service and unregister-service cannot be used together")
	}
	if registerServiceFlag {
		return true, registerService()
	}
	if unregisterServiceFlag {
		return true, unregisterService()
	}

	interactive, err := svc.IsAnInteractiveSession()
	if err != nil {
		return false, fmt.Errorf("detect Windows service session: %w", err)
	}
	if interactive {
		return false, nil
	}

	handler := &serviceHandler{started: make(chan struct{})}
	runError := make(chan error, 1)
	go func() {
		runError <- svc.Run(serviceName, handler)
	}()
	select {
	case <-handler.started:
		return false, nil
	case err := <-runError:
		return false, fmt.Errorf("run Windows service dispatcher: %w", err)
	}
}

// StopChannel is closed when the Windows service manager requests a stop or
// shutdown. In an interactive session it remains open until process exit.
func StopChannel() <-chan struct{} {
	return serviceStop
}

func registerService() (err error) {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate service executable: %w", err)
	}
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to Windows service manager: %w", err)
	}
	defer manager.Disconnect()

	config := mgr.Config{
		ServiceType:      windows.SERVICE_WIN32_OWN_PROCESS,
		StartType:        mgr.StartAutomatic,
		ErrorControl:     mgr.ErrorNormal,
		DisplayName:      serviceDisplayName,
		Description:      serviceDescription,
		DelayedAutoStart: true,
	}
	service, err := manager.CreateService(serviceName, executable, config, "--enable-route-update")
	if err != nil {
		return fmt.Errorf("create Windows service %q: %w", serviceName, err)
	}
	defer service.Close()

	created := true
	defer func() {
		if err != nil && created {
			_ = service.Delete()
		}
	}()
	actions := []mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 15 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
		{Type: mgr.NoAction},
	}
	if err = service.SetRecoveryActions(actions, uint32((24*time.Hour)/time.Second)); err != nil {
		return fmt.Errorf("configure Windows service recovery: %w", err)
	}
	if err = service.SetRecoveryActionsOnNonCrashFailures(true); err != nil {
		return fmt.Errorf("configure Windows service failure handling: %w", err)
	}
	created = false
	return nil
}

func unregisterService() error {
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to Windows service manager: %w", err)
	}
	defer manager.Disconnect()

	service, err := manager.OpenService(serviceName)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open Windows service %q: %w", serviceName, err)
	}
	defer service.Close()
	if err := service.Delete(); err != nil {
		return fmt.Errorf("delete Windows service %q: %w", serviceName, err)
	}
	return nil
}

type serviceHandler struct {
	started chan struct{}
}

func (handler *serviceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, statuses chan<- svc.Status) (bool, uint32) {
	statuses <- svc.Status{State: svc.StartPending}
	close(handler.started)
	accepts := svc.AcceptStop | svc.AcceptShutdown
	statuses <- svc.Status{State: svc.Running, Accepts: accepts}

	for request := range requests {
		switch request.Cmd {
		case svc.Interrogate:
			statuses <- request.CurrentStatus
		case svc.Stop, svc.Shutdown:
			statuses <- svc.Status{State: svc.StopPending}
			serviceStopOnce.Do(func() { close(serviceStop) })
			return false, 0
		}
	}
	serviceStopOnce.Do(func() { close(serviceStop) })
	return false, 0
}
