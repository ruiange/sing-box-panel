//go:build !linux

package panel

func readServerStatus() (serverStatus, cpuTicks) {
	return serverStatus{}, cpuTicks{}
}
